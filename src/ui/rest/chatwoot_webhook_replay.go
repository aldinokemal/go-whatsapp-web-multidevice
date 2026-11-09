package rest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
)

// chatwootReplayGuardMaxEntries bounds how many accepted signatures the replay
// guard remembers. See chatwootReplayGuard for what happens at the bound.
const chatwootReplayGuardMaxEntries = 1 << 16

// chatwootReplayInFlightWait is how long a signed request waits for an
// identical request that is still being handled before it is answered 503. It
// stays below the request timeout so the wait ends with an answer.
var chatwootReplayInFlightWait = 30 * time.Second

// chatwootReplayState is what the guard knows about a signed request.
type chatwootReplayState int

const (
	// chatwootReplayNew: not seen; the caller now holds the reservation and
	// must commit or release it.
	chatwootReplayNew chatwootReplayState = iota
	// chatwootReplayAccepted: an identical request was already accepted.
	chatwootReplayAccepted
	// chatwootReplayInFlight: an identical request is being handled right now.
	chatwootReplayInFlight
)

// chatwootReplayKey identifies one signed delivery: the signature covers the
// timestamp and the body and is keyed with the secret.
type chatwootReplayKey struct {
	timestamp int64
	digest    [sha256.Size]byte
}

// chatwootReplayOwner partitions the guard's memory by the secret a request
// verified against (its SHA-256, so the secret itself is not kept here).
type chatwootReplayOwner [sha256.Size]byte

type chatwootReplayEntry struct {
	key     chatwootReplayKey
	expires time.Time
}

// chatwootReplayGuard keeps a captured Chatwoot-signed request from being
// acted on twice, without swallowing a request that was never handled.
//
// A verified request first reserves its (timestamp, signature) pair. The
// reservation is committed only when the request was accepted (a 2xx answer)
// and released on any other outcome, so the same request sent again after a
// failure is handled. While a reservation is held, an identical request is not
// handled alongside it.
//
// Memory is bounded two ways:
//
//   - by age: an accepted pair is kept for twice the signature tolerance, which
//     covers every moment the timestamp check could still accept it;
//   - by count: at most max accepted pairs. Past that, the oldest pair of the
//     secret that currently holds the most pairs is forgotten.
//
// The count bound trades replay protection for availability: when it is hit,
// a pair is forgotten early (and could be replayed) rather than new webhooks
// being refused. Evicting from the largest holder limits who pays for that.
// Only verified signatures are remembered, so filling the guard takes a valid
// secret, and whoever does it evicts their own pairs once they hold the most:
// every other secret keeps at least its newest max/N pairs, N being the number
// of secrets with remembered pairs. A deployment with a single secret has one
// holder, and a flood under that secret forgets that secret's oldest pairs.
//
// Reservations are not counted against max and are never evicted; there is at
// most one per request being handled.
//
// It is in-process memory: it does not survive a restart and is not shared
// between instances.
type chatwootReplayGuard struct {
	mu       sync.Mutex
	max      int
	size     int
	accepted map[chatwootReplayKey]struct{}
	// queues hold each owner's accepted pairs in commit order, which is also
	// expiry order.
	queues map[chatwootReplayOwner][]chatwootReplayEntry
	// inFlight maps a reserved pair to a channel closed when it is settled.
	inFlight map[chatwootReplayKey]chan struct{}
}

var chatwootReplays = &chatwootReplayGuard{max: chatwootReplayGuardMaxEntries}

// chatwootReplayKeyFor parses a verified timestamp/signature pair.
func chatwootReplayKeyFor(timestamp, signature string) (chatwootReplayKey, bool) {
	var key chatwootReplayKey
	seconds, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return key, false
	}
	digest, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(signature), "sha256="))
	if err != nil || len(digest) != len(key.digest) {
		return key, false
	}
	key.timestamp = seconds
	copy(key.digest[:], digest)
	return key, true
}

// reserve reports what is known about key. On chatwootReplayNew the caller
// holds the reservation and must commit or release it. On
// chatwootReplayInFlight the returned channel is closed when the holder
// settles, after which reserve can be tried again.
func (g *chatwootReplayGuard) reserve(key chatwootReplayKey, now time.Time) (chatwootReplayState, <-chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.forgetExpired(now)
	if _, ok := g.accepted[key]; ok {
		return chatwootReplayAccepted, nil
	}
	if settled, ok := g.inFlight[key]; ok {
		return chatwootReplayInFlight, settled
	}
	if g.inFlight == nil {
		g.inFlight = make(map[chatwootReplayKey]chan struct{})
	}
	g.inFlight[key] = make(chan struct{})
	return chatwootReplayNew, nil
}

// commit turns a reservation into a remembered pair.
func (g *chatwootReplayGuard) commit(key chatwootReplayKey, owner chatwootReplayOwner, now time.Time, tolerance time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.settle(key)
	g.forgetExpired(now)
	if _, ok := g.accepted[key]; ok {
		return
	}
	if g.accepted == nil {
		g.accepted = make(map[chatwootReplayKey]struct{})
		g.queues = make(map[chatwootReplayOwner][]chatwootReplayEntry)
	}
	g.accepted[key] = struct{}{}
	// A timestamp may be up to tolerance ahead of this clock and is then
	// accepted until tolerance past it, so twice the tolerance from now covers
	// every accepted timestamp and keeps expiry in commit order.
	g.queues[owner] = append(g.queues[owner], chatwootReplayEntry{key: key, expires: now.Add(2 * tolerance)})
	g.size++
	for g.max > 0 && g.size > g.max {
		g.forgetOldestOf(g.largestOwner())
	}
}

// release drops a reservation without remembering the pair.
func (g *chatwootReplayGuard) release(key chatwootReplayKey) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.settle(key)
}

func (g *chatwootReplayGuard) settle(key chatwootReplayKey) {
	if settled, ok := g.inFlight[key]; ok {
		delete(g.inFlight, key)
		close(settled)
	}
}

// forgetExpired drops what the timestamp check now rejects by itself.
func (g *chatwootReplayGuard) forgetExpired(now time.Time) {
	for owner, queue := range g.queues {
		for len(queue) > 0 && !queue[0].expires.After(now) {
			g.forgetOldestOf(owner)
			queue = g.queues[owner]
		}
	}
}

func (g *chatwootReplayGuard) largestOwner() chatwootReplayOwner {
	var largest chatwootReplayOwner
	most := 0
	for owner, queue := range g.queues {
		if len(queue) > most {
			largest, most = owner, len(queue)
		}
	}
	return largest
}

func (g *chatwootReplayGuard) forgetOldestOf(owner chatwootReplayOwner) {
	queue := g.queues[owner]
	if len(queue) == 0 {
		return
	}
	delete(g.accepted, queue[0].key)
	g.size--
	if len(queue) == 1 {
		delete(g.queues, owner)
		return
	}
	g.queues[owner] = queue[1:]
}

// reset forgets every accepted pair. Reservations are left to their holders.
func (g *chatwootReplayGuard) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.accepted = nil
	g.queues = nil
	g.size = 0
}

// chatwootReplayRequestKey is the request-local slot ChatwootWebhookReplayGuard
// installs; it carries the reservation the request holds, if any.
type chatwootReplayRequestKey struct{}

type chatwootReplayRequest struct {
	held  bool
	key   chatwootReplayKey
	owner chatwootReplayOwner
}

// chatwootReplayOutcomeKey is the request-local chatwootReplayState set when
// the request must not be acted on: chatwootReplayAccepted or
// chatwootReplayInFlight. chatwootWebhookRouteGate turns it into the answer.
type chatwootReplayOutcomeKey struct{}

// ChatwootWebhookReplayGuard must be mounted in front of the Chatwoot webhook
// handlers. It settles the replay reservation a Chatwoot-signed request takes
// when its signature verifies, from the outcome of everything behind it: the
// signature is remembered only when the request was answered 2xx, and is
// released on any other status, a returned error, or a panic, so that the same
// request sent again is handled. Deciding here, and not inside a handler,
// keeps that true whatever the handlers do before they answer.
func ChatwootWebhookReplayGuard() fiber.Handler {
	return func(c fiber.Ctx) error {
		request := &chatwootReplayRequest{}
		c.Locals(chatwootReplayRequestKey{}, request)

		accepted := false
		defer func() {
			// Runs on a panic too, with accepted still false.
			if !request.held {
				return
			}
			if accepted {
				chatwootReplays.commit(request.key, request.owner, time.Now(), config.ChatwootWebhookSignatureTolerance)
			} else {
				chatwootReplays.release(request.key)
			}
		}()

		err := c.Next()
		accepted = chatwootResponseAccepted(c, err)
		return err
	}
}

// chatwootResponseAccepted reports whether the request ends in a 2xx answer.
// A returned error is answered by the error handler: with its own status when
// it is a *fiber.Error, otherwise as a server error.
func chatwootResponseAccepted(c fiber.Ctx, err error) bool {
	status := c.Response().StatusCode()
	if err != nil {
		var fiberErr *fiber.Error
		if !errors.As(err, &fiberErr) {
			return false
		}
		status = fiberErr.Code
	}
	return status >= 200 && status < 300
}

// chatwootReplayUnguardedWarning reports a missing ChatwootWebhookReplayGuard
// once instead of on every webhook.
var chatwootReplayUnguardedWarning sync.Once

// chatwootReplayAdmit is called once a request's Chatwoot signature verified
// against secret. It reserves the signature for this request, or records why
// the request must not be acted on. While an identical request is being
// handled it waits for that one to finish: accepted makes this one a repeat,
// anything else lets this one take its place.
func chatwootReplayAdmit(c fiber.Ctx, secret, timestamp, signature string) {
	key, ok := chatwootReplayKeyFor(timestamp, signature)
	if !ok {
		return
	}
	request, _ := c.Locals(chatwootReplayRequestKey{}).(*chatwootReplayRequest)
	if request != nil && request.held && request.key == key {
		// Verified a second time in the same request (another secret check).
		return
	}

	var deadline <-chan time.Time
	for {
		state, settled := chatwootReplays.reserve(key, time.Now())
		switch state {
		case chatwootReplayNew:
			owner := chatwootReplayOwner(sha256.Sum256([]byte(secret)))
			if request == nil {
				// No ChatwootWebhookReplayGuard in front of this handler, so
				// nothing would settle a reservation: remember the signature
				// now. A request that then fails is not handled when resent.
				chatwootReplayUnguardedWarning.Do(func() {
					logrus.Error("Chatwoot Webhook: ChatwootWebhookReplayGuard is not mounted in front of the webhook handler; " +
						"a signed request that fails will not be handled when it is sent again")
				})
				chatwootReplays.commit(key, owner, time.Now(), config.ChatwootWebhookSignatureTolerance)
				return
			}
			request.held, request.key, request.owner = true, key, owner
			return
		case chatwootReplayAccepted:
			c.Locals(chatwootReplayOutcomeKey{}, chatwootReplayAccepted)
			return
		}

		if deadline == nil {
			timer := time.NewTimer(chatwootReplayInFlightWait)
			defer timer.Stop()
			deadline = timer.C
		}
		select {
		case <-settled:
		case <-deadline:
			c.Locals(chatwootReplayOutcomeKey{}, chatwootReplayInFlight)
			return
		}
	}
}
