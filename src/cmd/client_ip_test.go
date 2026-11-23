package cmd

import (
	"io"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// clientIPServer serves GET /ip (echoing c.IP()) and GET /hit (one-per-IP
// limiter, the keying the CIMD limiter uses) on loopback, configured the way
// restServer configures multi-user mode behind a proxy. Peers are 127.0.0.1.
func clientIPServer(t *testing.T, trusted []string) string {
	t.Helper()
	cfg := fiber.Config{TrustProxy: true}
	applyTrustedProxies(&cfg, trusted, true)
	app := fiber.New(cfg)
	var mu sync.Mutex
	seen := map[string]bool{}
	app.Get("/ip", func(c fiber.Ctx) error { return c.SendString(c.IP()) })
	app.Get("/hit", func(c fiber.Ctx) error {
		mu.Lock()
		defer mu.Unlock()
		if seen[c.IP()] {
			return c.SendStatus(http.StatusTooManyRequests)
		}
		seen[c.IP()] = true
		return c.SendStatus(http.StatusOK)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	return "http://" + ln.Addr().String()
}

func get(t *testing.T, url, path, xff string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+path, nil)
	require.NoError(t, err)
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestTrustedPeerGivesTheRealClientFromTheChain(t *testing.T) {
	url := clientIPServer(t, []string{"127.0.0.0/8", "10.0.0.0/8"})
	// A spoofed left entry, then the real client, then an internal hop.
	_, ip := get(t, url, "/ip", "6.6.6.6, 203.0.113.7, 10.1.2.3")
	require.Equal(t, "203.0.113.7", ip, "right-most entry that is not a trusted proxy")
}

func TestUntrustedPeerCannotSetItsAddress(t *testing.T) {
	url := clientIPServer(t, []string{"10.0.0.0/8"}) // the test peer 127.0.0.1 is not trusted
	_, ip := get(t, url, "/ip", "6.6.6.6")
	require.Equal(t, "127.0.0.1", ip)
}

func TestTwoClientsBehindTheProxyHaveSeparateBuckets(t *testing.T) {
	url := clientIPServer(t, []string{"127.0.0.0/8"})
	code, _ := get(t, url, "/hit", "203.0.113.1")
	require.Equal(t, http.StatusOK, code)
	code, _ = get(t, url, "/hit", "203.0.113.2")
	require.Equal(t, http.StatusOK, code, "a second client is not limited by the first")
	code, _ = get(t, url, "/hit", "203.0.113.1")
	require.Equal(t, http.StatusTooManyRequests, code, "the same client is")
}

func TestSingleUserProxyHeaderIsUnchanged(t *testing.T) {
	cfg := fiber.Config{}
	applyTrustedProxies(&cfg, []string{"10.0.0.0/8"}, false)
	require.Equal(t, fiber.HeaderXForwardedHost, cfg.ProxyHeader)
	require.False(t, cfg.EnableIPValidation)
}
