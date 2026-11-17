package websocket

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	fiberws "github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	go RunHub()
	m.Run()
}

type recordingConn struct {
	mu   sync.Mutex
	msgs [][]byte
}

func (c *recordingConn) WriteMessage(_ int, data []byte) error {
	c.mu.Lock()
	c.msgs = append(c.msgs, append([]byte(nil), data...))
	c.mu.Unlock()
	return nil
}

func (c *recordingConn) SetWriteDeadline(time.Time) error { return nil }

func (c *recordingConn) Close() error { return nil }

func (c *recordingConn) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.msgs)
}

type blockingConn struct {
	mu       sync.Mutex
	deadline time.Time
	once     sync.Once
	done     chan struct{}
}

func newBlockingConn() *blockingConn {
	return &blockingConn{done: make(chan struct{})}
}

func (c *blockingConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}

func (c *blockingConn) WriteMessage(int, []byte) error {
	c.mu.Lock()
	deadline := c.deadline
	c.mu.Unlock()
	var timer <-chan time.Time
	if !deadline.IsZero() {
		timer = time.After(time.Until(deadline))
	}
	select {
	case <-timer:
		return errors.New("write deadline exceeded")
	case <-c.done:
		return errors.New("stalled client disconnected")
	}
}

func (c *blockingConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func waitForClients(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		clientsMu.Lock()
		got := len(clients)
		clientsMu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d websocket clients", n)
}

// waitForSockets waits until exactly n subscribers on a real websocket are
// registered. The fake connections of the hub tests are not counted.
func waitForSockets(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := 0
		clientsMu.Lock()
		for sub := range clients {
			if _, ok := sub.conn.(*fiberws.Conn); ok {
				got++
			}
		}
		clientsMu.Unlock()
		if got == n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d websocket connections", n)
}

func TestHubDoesNotBlockWhenClientStopsReading(t *testing.T) {
	prevTimeout := writeTimeout
	prevQueue := clientQueueSize
	// Longer than the publish timeout so a hub that writes synchronously fails the test.
	writeTimeout = 2 * time.Second
	clientQueueSize = 1
	t.Cleanup(func() {
		writeTimeout = prevTimeout
		clientQueueSize = prevQueue
	})

	stalled := newBlockingConn()
	healthy := &recordingConn{}
	// The healthy queue holds both broadcasts, so only the stalled client can
	// be dropped as slow.
	subs := []*subscriber{newSubscriber(stalled), {conn: healthy, send: make(chan []byte, 2), done: make(chan struct{})}}
	// Runs before the cleanup above: the writers read writeTimeout, so they
	// must exit before it is restored.
	t.Cleanup(func() {
		for _, sub := range subs {
			sub.failSlow()
			<-sub.done
		}
	})

	for _, sub := range subs {
		Register <- sub
	}
	waitForClients(t, 2)

	send := func() time.Duration {
		t.Helper()
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		if !Publish(ctx, BroadcastMessage{Code: "PING", Message: "still going"}) {
			t.Fatal("publish blocked or was cancelled while a client was stalled")
		}
		return time.Since(start)
	}

	if elapsed := send(); elapsed > 150*time.Millisecond {
		t.Fatalf("first publish took %s; the hub waited on the stalled client", elapsed)
	}
	if elapsed := send(); elapsed > 150*time.Millisecond {
		t.Fatalf("second publish took %s; a full or stalled client blocked the hub", elapsed)
	}

	deadline := time.Now().Add(time.Second)
	for healthy.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if healthy.count() < 2 {
		t.Fatalf("healthy client received %d of 2 broadcasts while another client was stalled", healthy.count())
	}
}

func TestWebsocketRejectsUntrustedOrigin(t *testing.T) {
	app := fiber.New()
	RegisterRoutes(app, nil)

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
	})
	go func() {
		_ = app.Listener(ln, fiber.ListenConfig{
			DisableStartupMessage: true,
			GracefulContext:       ctx,
			ShutdownTimeout:       time.Second,
		})
	}()

	endpoint := "ws://" + ln.Addr().String() + "/ws"
	dialer := websocket.Dialer{HandshakeTimeout: time.Second}
	var untrusted *websocket.Conn
	var resp *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for {
		untrusted, resp, err = dialer.Dial(endpoint, http.Header{"Origin": []string{"https://evil.example"}})
		if err == nil || resp != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if untrusted != nil {
		_ = untrusted.Close()
	}
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, fiber.StatusForbidden, resp.StatusCode)
	_ = resp.Body.Close()

	trustedOrigin := "http://" + ln.Addr().String()
	conn, resp, err := dialer.Dial(endpoint, http.Header{"Origin": []string{trustedOrigin}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err)
	require.NotNil(t, conn)

	// A TLS-terminating proxy forwards the dashboard's https origin over http.
	proxied, resp, err := dialer.Dial(endpoint, http.Header{"Origin": []string{"https://" + ln.Addr().String()}})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err)
	require.NotNil(t, proxied)

	missing, resp, err := dialer.Dial(endpoint, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	require.NoError(t, err)
	require.NotNil(t, missing)

	// Each server handler must register and exit before the next test
	// changes the globals it reads.
	waitForSockets(t, 3)
	_ = conn.Close()
	_ = proxied.Close()
	_ = missing.Close()
	waitForSockets(t, 0)
}
