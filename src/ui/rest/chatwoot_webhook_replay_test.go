package rest

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
)

const replayTestSecret = "channel-secret"

// newReplayGuardApp mounts the replay guard in front of a stand-in webhook
// handler: it authorizes the request the way the real handlers do, then hands
// over to handle, which decides the answer. The guard has to work from that
// answer alone.
func newReplayGuardApp(t *testing.T, handle fiber.Handler) *fiber.App {
	t.Helper()
	withChatwootWebhookSecret(t, replayTestSecret)
	chatwootReplays.reset()
	t.Cleanup(chatwootReplays.reset)

	app := fiber.New()
	app.Use(middleware.Recovery())
	app.Use("/chatwoot/webhook", ChatwootWebhookReplayGuard())
	app.Post("/chatwoot/webhook", func(c fiber.Ctx) error {
		if status := (&ChatwootHandler{}).chatwootWebhookRouteGate(c, chatwootWebhookRoute{Unroutable: true}); status != 0 {
			return c.SendStatus(status)
		}
		return handle(c)
	})
	return app
}

// replayTestBody makes every test's signed request distinct.
var replayTestBody atomic.Int64

func signedReplayRequest() webhookAuthRequest {
	body := fmt.Sprintf(`{"event":"message_created","id":%d}`, replayTestBody.Add(1))
	return webhookAuthRequest{body: body, headers: chatwootSigned(replayTestSecret, body)}
}

// postReplayRequest is postChatwootWebhook for requests that are held open on
// purpose, usable from several goroutines.
func postReplayRequest(app *fiber.App, r webhookAuthRequest) (int, error) {
	req := httptest.NewRequest(http.MethodPost, "/chatwoot/webhook", strings.NewReader(r.body))
	req.Header.Set("Content-Type", "application/json")
	for name, value := range r.headers {
		req.Header.Set(name, value)
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second, FailOnTimeout: true})
	if err != nil {
		return 0, err
	}
	return resp.StatusCode, nil
}

// TestChatwootReplayGuardRemembersOnlyAcceptedRequests: whatever stands behind
// the guard, a signed request that was not answered 2xx was not handled, so
// the identical request sent again reaches the handler. Once it is answered
// 2xx, a further copy is acknowledged without reaching the handler.
func TestChatwootReplayGuardRemembersOnlyAcceptedRequests(t *testing.T) {
	for _, tt := range []struct {
		name       string
		fail       func(c fiber.Ctx) error
		wantStatus int
	}{
		{name: "answers 503", fail: func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusServiceUnavailable) }, wantStatus: 503},
		{name: "answers 400", fail: func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusBadRequest) }, wantStatus: 400},
		{name: "returns an HTTP error", fail: func(fiber.Ctx) error { return fiber.NewError(fiber.StatusServiceUnavailable, "queue unavailable") }, wantStatus: 503},
		{name: "returns an error", fail: func(fiber.Ctx) error { return errors.New("database is locked") }, wantStatus: 500},
		{name: "returns an error after writing 200", fail: func(c fiber.Ctx) error {
			c.Status(fiber.StatusOK)
			return errors.New("database is locked")
		}, wantStatus: 500},
		{name: "panics", fail: func(fiber.Ctx) error { panic("handler crashed") }, wantStatus: 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handled := 0
			app := newReplayGuardApp(t, func(c fiber.Ctx) error {
				handled++
				if handled == 1 {
					return tt.fail(c)
				}
				return c.SendStatus(fiber.StatusOK)
			})
			request := signedReplayRequest()

			if got := postChatwootWebhook(t, app, request); got != tt.wantStatus {
				t.Fatalf("first delivery: status = %d, want %d", got, tt.wantStatus)
			}
			if got := postChatwootWebhook(t, app, request); got != fiber.StatusOK {
				t.Fatalf("redelivery: status = %d, want 200", got)
			}
			if handled != 2 {
				t.Fatalf("handler ran %d times, want 2: the redelivery of a failed request must be handled", handled)
			}
			if got := postChatwootWebhook(t, app, request); got != fiber.StatusOK {
				t.Fatalf("repeat: status = %d, want 200", got)
			}
			if handled != 2 {
				t.Fatalf("handler ran %d times, want 2: a copy of an accepted request must not be handled", handled)
			}
		})
	}
}

// TestChatwootReplayGuardConcurrentDuplicates: identical signed requests
// arriving while the first is still being handled are not handled alongside
// it. They wait for its outcome and are acknowledged as repeats.
func TestChatwootReplayGuardConcurrentDuplicates(t *testing.T) {
	var handled atomic.Int32
	entered := make(chan struct{}, 16)
	finish := make(chan struct{})
	app := newReplayGuardApp(t, func(c fiber.Ctx) error {
		handled.Add(1)
		entered <- struct{}{}
		<-finish
		return c.SendStatus(fiber.StatusOK)
	})
	request := signedReplayRequest()

	const copies = 8
	statuses := make(chan int, copies)
	failures := make(chan error, copies)
	var wg sync.WaitGroup
	for i := 0; i < copies; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, err := postReplayRequest(app, request)
			if err != nil {
				failures <- err
				return
			}
			statuses <- status
		}()
	}

	<-entered
	// Give the other copies time to reach the guard while the first is held;
	// the assertions below hold whether or not they have.
	time.Sleep(50 * time.Millisecond)
	if got := handled.Load(); got != 1 {
		t.Errorf("%d copies were being handled at once, want 1", got)
	}
	close(finish)
	wg.Wait()
	close(statuses)
	close(failures)

	for err := range failures {
		t.Fatalf("request failed: %v", err)
	}
	for status := range statuses {
		if status != fiber.StatusOK {
			t.Errorf("status = %d, want 200", status)
		}
	}
	if got := handled.Load(); got != 1 {
		t.Fatalf("handler ran %d times for %d identical requests, want 1", got, copies)
	}
}

// TestChatwootReplayGuardDuplicateTakesOverAfterFailure: a duplicate waiting
// on a request that then fails is handled in its place.
func TestChatwootReplayGuardDuplicateTakesOverAfterFailure(t *testing.T) {
	var handled atomic.Int32
	entered := make(chan struct{}, 2)
	finish := make(chan struct{})
	app := newReplayGuardApp(t, func(c fiber.Ctx) error {
		if handled.Add(1) == 1 {
			entered <- struct{}{}
			<-finish
			return c.SendStatus(fiber.StatusServiceUnavailable)
		}
		return c.SendStatus(fiber.StatusOK)
	})
	request := signedReplayRequest()

	type result struct {
		status int
		err    error
	}
	first, second := make(chan result, 1), make(chan result, 1)
	go func() {
		status, err := postReplayRequest(app, request)
		first <- result{status, err}
	}()
	<-entered
	go func() {
		status, err := postReplayRequest(app, request)
		second <- result{status, err}
	}()
	time.Sleep(50 * time.Millisecond)
	if got := handled.Load(); got != 1 {
		t.Errorf("%d copies were being handled at once, want 1", got)
	}
	close(finish)

	if got := <-first; got.err != nil || got.status != fiber.StatusServiceUnavailable {
		t.Fatalf("first: status = %d, err = %v, want 503", got.status, got.err)
	}
	if got := <-second; got.err != nil || got.status != fiber.StatusOK {
		t.Fatalf("second: status = %d, err = %v, want 200", got.status, got.err)
	}
	if got := handled.Load(); got != 2 {
		t.Fatalf("handler ran %d times, want 2: the duplicate must be handled after the first failed", got)
	}
}

// TestChatwootReplayGuardDuplicateDoesNotWaitForever: a duplicate of a request
// that stays in flight is refused, not handled and not acknowledged.
func TestChatwootReplayGuardDuplicateDoesNotWaitForever(t *testing.T) {
	previous := chatwootReplayInFlightWait
	chatwootReplayInFlightWait = 20 * time.Millisecond
	t.Cleanup(func() { chatwootReplayInFlightWait = previous })

	var handled atomic.Int32
	entered := make(chan struct{}, 2)
	finish := make(chan struct{})
	app := newReplayGuardApp(t, func(c fiber.Ctx) error {
		handled.Add(1)
		entered <- struct{}{}
		<-finish
		return c.SendStatus(fiber.StatusOK)
	})
	request := signedReplayRequest()

	first := make(chan error, 1)
	go func() {
		status, err := postReplayRequest(app, request)
		if err == nil && status != fiber.StatusOK {
			err = fmt.Errorf("status = %d, want 200", status)
		}
		first <- err
	}()
	<-entered

	status, err := postReplayRequest(app, request)
	if err != nil || status != fiber.StatusServiceUnavailable {
		t.Errorf("duplicate of an in-flight request: status = %d, err = %v, want 503", status, err)
	}
	if got := handled.Load(); got != 1 {
		t.Errorf("handler ran %d times, want 1", got)
	}
	close(finish)
	if err := <-first; err != nil {
		t.Fatalf("first: %v", err)
	}
}

func replayTestKey(t *testing.T, timestamp string, n int) chatwootReplayKey {
	t.Helper()
	key, ok := chatwootReplayKeyFor(timestamp, chatwootSign("secret", timestamp, fmt.Sprintf("body-%d", n)))
	if !ok {
		t.Fatalf("no key for body %d", n)
	}
	return key
}

// accept runs a pair through the guard as an accepted request would.
func (g *chatwootReplayGuard) accept(t *testing.T, key chatwootReplayKey, owner chatwootReplayOwner, now time.Time, tolerance time.Duration) {
	t.Helper()
	if state, _ := g.reserve(key, now); state != chatwootReplayNew {
		t.Fatalf("reserve = %v, want a new reservation", state)
	}
	g.commit(key, owner, now, tolerance)
}

func (g *chatwootReplayGuard) remembers(key chatwootReplayKey, now time.Time) bool {
	state, _ := g.reserve(key, now)
	if state == chatwootReplayNew {
		g.release(key)
	}
	return state == chatwootReplayAccepted
}

func TestChatwootReplayGuard(t *testing.T) {
	const tolerance = 5 * time.Minute
	now := time.Unix(1700000000, 0)
	ownerA := chatwootReplayOwner(sha256.Sum256([]byte("secret-a")))
	ownerB := chatwootReplayOwner(sha256.Sum256([]byte("secret-b")))
	key := func(n int) chatwootReplayKey { return replayTestKey(t, "1700000000", n) }

	t.Run("a reservation is exclusive until it is settled", func(t *testing.T) {
		g := &chatwootReplayGuard{max: 8}
		if state, _ := g.reserve(key(1), now); state != chatwootReplayNew {
			t.Fatalf("first reserve = %v, want new", state)
		}
		state, settled := g.reserve(key(1), now)
		if state != chatwootReplayInFlight {
			t.Fatalf("second reserve = %v, want in flight", state)
		}
		select {
		case <-settled:
			t.Fatal("reservation reported settled while held")
		default:
		}

		g.release(key(1))
		select {
		case <-settled:
		default:
			t.Fatal("release did not wake the waiting request")
		}
		if g.size != 0 || len(g.inFlight) != 0 {
			t.Fatalf("a released reservation left size=%d inFlight=%d, want nothing", g.size, len(g.inFlight))
		}

		// Released, so the pair can be reserved again, and committed this time.
		if state, _ := g.reserve(key(1), now); state != chatwootReplayNew {
			t.Fatalf("reserve after release = %v, want new", state)
		}
		_, settled = g.reserve(key(1), now)
		g.commit(key(1), ownerA, now, tolerance)
		select {
		case <-settled:
		default:
			t.Fatal("commit did not wake the waiting request")
		}
		if state, _ := g.reserve(key(1), now); state != chatwootReplayAccepted {
			t.Fatalf("reserve after commit = %v, want accepted", state)
		}
		if len(g.inFlight) != 0 {
			t.Fatalf("%d reservations left behind", len(g.inFlight))
		}
	})

	t.Run("remembers a pair until the timestamp check rejects it", func(t *testing.T) {
		g := &chatwootReplayGuard{max: 8}
		g.accept(t, key(1), ownerA, now, tolerance)
		if !g.remembers(key(1), now.Add(tolerance)) {
			t.Fatal("accepted pair not remembered inside the window")
		}
		if g.remembers(replayTestKey(t, "1700000001", 1), now) {
			t.Fatal("same body with another timestamp reported as a repeat")
		}
		// A timestamp tolerance ahead of this clock is accepted until
		// 2*tolerance from now; the entry must live that long.
		if !g.remembers(key(1), now.Add(2*tolerance-time.Second)) {
			t.Fatal("pair forgotten while its timestamp could still be accepted")
		}
		if g.remembers(key(1), now.Add(2*tolerance)) {
			t.Fatal("expired pair reported as a repeat")
		}
		if g.size != 0 || len(g.accepted) != 0 || len(g.queues) != 0 {
			t.Fatalf("expired pairs kept: size=%d accepted=%d queues=%d, want none", g.size, len(g.accepted), len(g.queues))
		}
	})

	t.Run("is bounded and forgets a secret's oldest pairs first", func(t *testing.T) {
		g := &chatwootReplayGuard{max: 3}
		for n := 1; n <= 50; n++ {
			g.accept(t, key(n), ownerA, now, tolerance)
			if g.size > 3 || len(g.accepted) > 3 || len(g.queues[ownerA]) > 3 {
				t.Fatalf("guard grew to size=%d accepted=%d queue=%d, want at most 3", g.size, len(g.accepted), len(g.queues[ownerA]))
			}
		}
		if !g.remembers(key(50), now) || !g.remembers(key(48), now) {
			t.Fatal("newest pairs were forgotten")
		}
		if g.remembers(key(47), now) {
			t.Fatal("oldest pair survived past the bound")
		}
	})

	t.Run("a flood under one secret does not evict another secret's recent pairs", func(t *testing.T) {
		g := &chatwootReplayGuard{max: 8}
		for n := 1; n <= 6; n++ {
			g.accept(t, key(n), ownerA, now, tolerance)
		}
		for n := 100; n < 1100; n++ {
			g.accept(t, key(n), ownerB, now, tolerance)
			if g.size > 8 || len(g.accepted) > 8 {
				t.Fatalf("guard grew to size=%d accepted=%d, want at most 8", g.size, len(g.accepted))
			}
		}
		// Two secrets share eight slots: the flooded-out secret keeps its
		// newest four, and the flood has been evicting its own pairs since.
		for n := 3; n <= 6; n++ {
			if !g.remembers(key(n), now) {
				t.Fatalf("pair %d of the other secret was evicted by the flood", n)
			}
		}
		if g.remembers(key(1), now) || g.remembers(key(2), now) {
			t.Fatal("the other secret kept more than its share")
		}
		if got := len(g.queues[ownerA]); got != 4 {
			t.Fatalf("the other secret keeps %d pairs, want 4", got)
		}
	})

	t.Run("reservations are not evicted", func(t *testing.T) {
		g := &chatwootReplayGuard{max: 2}
		if state, _ := g.reserve(key(1), now); state != chatwootReplayNew {
			t.Fatalf("reserve = %v, want new", state)
		}
		for n := 2; n <= 20; n++ {
			g.accept(t, key(n), ownerA, now, tolerance)
		}
		if state, _ := g.reserve(key(1), now); state != chatwootReplayInFlight {
			t.Fatalf("reserve of a held pair = %v, want in flight", state)
		}
	})

	t.Run("ignores what cannot have verified", func(t *testing.T) {
		for _, pair := range [][2]string{{"yesterday", chatwootSign("secret", "1700000000", "body")}, {"1700000000", "sha256=zz"}, {"1700000000", "sha256=00"}} {
			if _, ok := chatwootReplayKeyFor(pair[0], pair[1]); ok {
				t.Fatalf("malformed pair %v produced a key", pair)
			}
		}
		spaced, ok := chatwootReplayKeyFor(" 1700000000 ", " "+chatwootSign("secret", "1700000000", "body-1")+" ")
		if !ok || spaced != key(1) {
			t.Fatal("surrounding whitespace changed the key")
		}
	})

	t.Run("concurrent reservations yield exactly one holder", func(t *testing.T) {
		g := &chatwootReplayGuard{max: 64}
		var wg sync.WaitGroup
		var holders atomic.Int32
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if state, _ := g.reserve(key(1), now); state == chatwootReplayNew {
					holders.Add(1)
				}
			}()
		}
		wg.Wait()
		if got := holders.Load(); got != 1 {
			t.Fatalf("%d goroutines hold the reservation, want exactly 1", got)
		}
	})
}
