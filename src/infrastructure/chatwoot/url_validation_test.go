package chatwoot

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

func TestCanonicalizeChatwootURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"https plain", "https://app.chatwoot.com", "https://app.chatwoot.com", false},
		{"trailing slash", "https://app.chatwoot.com/", "https://app.chatwoot.com", false},
		{"uppercase scheme+host", "HTTPS://APP.Chatwoot.COM", "https://app.chatwoot.com", false},
		{"strip default https port", "https://app.chatwoot.com:443", "https://app.chatwoot.com", false},
		{"strip default http port", "http://example.com:80/", "http://example.com", false},
		{"keep nonstandard port", "https://host.example.com:8443", "https://host.example.com:8443", false},
		{"keep subpath, trim slash", "https://h.example.com/chatwoot/", "https://h.example.com/chatwoot", false},
		{"surrounding spaces", "  https://app.chatwoot.com  ", "https://app.chatwoot.com", false},
		{"reject ftp", "ftp://example.com", "", true},
		{"reject empty", "", "", true},
		{"reject no host", "https://", "", true},
		{"reject userinfo", "https://user:pass@example.com", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalizeChatwootURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("canonicalize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateChatwootURL_SSRF(t *testing.T) {
	// Stub DNS so resolution is deterministic and hermetic (no network).
	orig := lookupIP
	t.Cleanup(func() { lookupIP = orig })
	lookupIP = func(host string) ([]net.IP, error) {
		switch host {
		case "app.chatwoot.com", "chat.example.com":
			return []net.IP{net.ParseIP("203.0.113.10")}, nil // public
		case "internal.example.com":
			return []net.IP{net.ParseIP("10.0.0.5")}, nil // resolves to private (SSRF via DNS)
		case "rebind.example.com":
			return []net.IP{net.ParseIP("203.0.113.7"), net.ParseIP("127.0.0.1")}, nil // one bad IP
		}
		return nil, fmt.Errorf("no such host: %s", host)
	}

	reject := []string{
		"http://127.0.0.1",
		"http://localhost",
		"http://10.0.0.5",
		"http://192.168.1.1:3000",
		"http://172.16.5.5",
		"http://169.254.169.254", // cloud metadata
		"http://[::1]",
		"http://0.0.0.0",
		"ftp://example.com",
		"https://user:pass@example.com",
		"https://internal.example.com", // hostname resolving to a private IP
		"https://rebind.example.com",   // any resolved IP private -> reject
		"https://nonexistent.invalid",  // unresolvable
	}
	for _, u := range reject {
		if err := ValidateChatwootURL(u); err == nil {
			t.Errorf("ValidateChatwootURL(%q) = nil, want rejection", u)
		}
	}

	accept := []string{
		"https://app.chatwoot.com",
		"https://chat.example.com:8443",
		"http://203.0.113.10", // public IP literal
	}
	for _, u := range accept {
		if err := ValidateChatwootURL(u); err != nil {
			t.Errorf("ValidateChatwootURL(%q) = %v, want accept", u, err)
		}
	}
}

// TestChatwootClientSSRFGuardWiring verifies per-device clients keep the
// connect-time guard whether or not an allowlist is set. The env client stays
// unguarded so a private self-host on CHATWOOT_URL keeps working. A stored URL
// that is no longer on the allowlist is not a usable client.
func TestChatwootClientSSRFGuardWiring(t *testing.T) {
	origHosts := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = origHosts })

	config.ChatwootAllowedHosts = nil
	if c := NewClientFromConfig("https://x.example.com", "t", 1, 1); c.HTTPClient.Transport == nil {
		t.Error("per-device client without allowlist should be SSRF-guarded (Transport set)")
	}
	if c := NewClient(); c.HTTPClient.Transport != nil {
		t.Error("env client should NOT be SSRF-guarded (trusted, may be internal)")
	}

	config.ChatwootAllowedHosts = []string{"x.example.com"}
	allowed := NewClientFromConfig("https://x.example.com", "t", 1, 1)
	if allowed.HTTPClient == nil || allowed.HTTPClient.Transport == nil {
		t.Error("per-device client with allowlist should still filter connections")
	}
	if !allowed.IsConfigured() {
		t.Error("allowlisted stored URL should stay usable")
	}
	removed := NewClientFromConfig("https://old.example.net", "t", 1, 1)
	if removed.IsConfigured() {
		t.Error("stored URL removed from the allowlist must not be usable")
	}

	// Sanity: the guard transport is a real *http.Transport.
	if _, ok := any(ssrfGuardedTransport()).(*http.Transport); !ok {
		t.Error("ssrfGuardedTransport should return an *http.Transport")
	}
}

func getWithContext(t *testing.T, c *http.Client, target string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c.Do(req)
}

// TestChatwootClientSSRFGuardBlocksConnect exercises the Dialer.Control guard
// end to end: a guarded client must refuse to actually connect to a loopback
// address (the DNS-rebinding case a one-time URL validation can't catch),
// while the unguarded env client connects to the same address. This covers the
// runtime behavior, not just that a Transport is wired.
func TestChatwootClientSSRFGuardBlocksConnect(t *testing.T) {
	origHosts := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = origHosts })
	config.ChatwootAllowedHosts = nil

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close() // listens on 127.0.0.1

	// Guarded per-device client: connect to the loopback server must be blocked.
	// It is named by host, since Proxy already refuses the literal IP.
	guarded := NewClientFromConfig(srv.URL, "t", 1, 1)
	blockedResp, err := getWithContext(t, guarded.HTTPClient, strings.Replace(srv.URL, "127.0.0.1", "localhost", 1))
	if err == nil {
		blockedResp.Body.Close()
		t.Fatal("guarded client connected to a loopback address, want block")
	}
	if !strings.Contains(err.Error(), "disallowed address") {
		t.Fatalf("guarded client error = %v, want a disallowed-address block", err)
	}

	// Unguarded env client: the same loopback connection succeeds (trusted).
	unguarded := NewClient()
	unguarded.HTTPClient.Timeout = 5 * time.Second
	resp, err := unguarded.HTTPClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("unguarded env client should reach loopback, got %v", err)
	}
	resp.Body.Close()
}

func TestValidateChatwootURL_Allowlist(t *testing.T) {
	orig := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = orig })

	config.ChatwootAllowedHosts = []string{"app.chatwoot.com"}
	if err := ValidateChatwootURL("https://app.chatwoot.com"); err != nil {
		t.Errorf("allowlisted host rejected: %v", err)
	}
	if err := ValidateChatwootURL("https://evil.example.com"); err == nil {
		t.Error("non-allowlisted host accepted, want rejection")
	}
}

func TestChatwootAllowlistPermitsPrivateHostAndRejectsRedirect(t *testing.T) {
	origHosts := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = origHosts })
	config.ChatwootAllowedHosts = []string{"127.0.0.1"}

	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okSrv.Close()

	allowed := NewClientFromConfig(okSrv.URL, "t", 1, 1)
	resp, err := getWithContext(t, allowed.HTTPClient, okSrv.URL)
	if err != nil {
		t.Fatalf("allowlisted loopback should connect: %v", err)
	}
	resp.Body.Close()

	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example/steal", http.StatusFound)
	}))
	defer redirectSrv.Close()

	// CheckRedirect's own error: the transport would refuse the host too, but
	// with another message, so this fails if CheckRedirect is not wired.
	redirected := NewClientFromConfig(redirectSrv.URL, "t", 1, 1)
	redirectResp, err := getWithContext(t, redirected.HTTPClient, redirectSrv.URL+"/v1")
	if err == nil {
		redirectResp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), `redirect host "evil.example" is not in the allowed hosts list`) {
		t.Fatalf("redirect off the allowlist: err=%v", err)
	}
}

// TestChatwootRedirectToAnotherHostDropsToken: the API token is for the
// configured Chatwoot host only. A redirect to another host must not carry
// it, with or without an allowlist.
func TestChatwootRedirectToAnotherHostDropsToken(t *testing.T) {
	origHosts := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = origHosts })

	tokens := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		tokens <- r.Header.Get("api_access_token")
	}))
	defer target.Close()
	// The same listener under another host name.
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, targetURL, http.StatusFound)
	}))
	defer origin.Close()

	for _, tt := range []struct {
		name    string
		allowed []string
		client  func() *http.Client
	}{
		// The guarded transport refuses loopback without an allowlist, so this
		// case runs the redirect policy on a plain transport.
		{"empty allowlist", nil, func() *http.Client { return &http.Client{CheckRedirect: chatwootCheckRedirect} }},
		{"both hosts allowlisted", []string{"127.0.0.1", "localhost"}, func() *http.Client { return NewClientFromConfig(origin.URL, "t", 1, 1).HTTPClient }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config.ChatwootAllowedHosts = tt.allowed
			req, err := http.NewRequest(http.MethodGet, origin.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("api_access_token", "secret-token")
			resp, err := tt.client().Do(req)
			if err != nil {
				t.Fatalf("redirect: %v", err)
			}
			resp.Body.Close()
			if got := <-tokens; got != "" {
				t.Fatalf("redirect target received api_access_token %q", got)
			}
		})
	}
}

// TestChatwootAllowlistChecksRequestHostNotDialedAddress: behind
// HTTP(S)_PROXY the dialed address is the proxy, which is not on the
// allowlist. The allowlist applies to the request's destination host.
func TestChatwootAllowlistChecksRequestHostNotDialedAddress(t *testing.T) {
	origHosts := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = origHosts })
	config.ChatwootAllowedHosts = []string{"chatwoot.example"}

	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer proxy.Close()

	tr := ssrfGuardedTransport()
	conn, err := tr.DialContext(context.Background(), "tcp", proxy.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dialing a proxy that is not on the allowlist: %v", err)
	}
	conn.Close()

	for host, wantErr := range map[string]bool{"chatwoot.example": false, "evil.example": true} {
		req, err := http.NewRequest(http.MethodGet, "http://"+host+"/api/v1", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Proxy(req); (err != nil) != wantErr {
			t.Errorf("Proxy(%s) err = %v, want error=%v", host, err, wantErr)
		}
	}
}

// TestChatwootProxyRejectsLiteralPrivateIPWithoutAllowlist: behind
// HTTP(S)_PROXY the dial guard sees only the proxy, so a request or redirect
// to a literal private or metadata IP must fail in Proxy, before the
// environment's proxy is chosen.
func TestChatwootProxyRejectsLiteralPrivateIPWithoutAllowlist(t *testing.T) {
	origHosts := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = origHosts })
	config.ChatwootAllowedHosts = nil

	tr := ssrfGuardedTransport()
	for target, wantErr := range map[string]bool{
		"http://169.254.169.254/latest/meta-data/": true,
		"http://[fd00::1]/api/v1":                  true,
		"http://203.0.113.10/api/v1":               false,
		"http://chatwoot.example/api/v1":           false,
	} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Proxy(req); (err != nil) != wantErr {
			t.Errorf("Proxy(%s) err = %v, want error=%v", target, err, wantErr)
		}
	}
}

// TestPerDeviceClientCapsDecompressedBody: a small gzip body from a
// per-device URL must not expand past maxChatwootResponseBody when read.
func TestPerDeviceClientCapsDecompressedBody(t *testing.T) {
	origHosts := config.ChatwootAllowedHosts
	t.Cleanup(func() { config.ChatwootAllowedHosts = origHosts })
	config.ChatwootAllowedHosts = []string{"127.0.0.1"}

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(make([]byte, maxChatwootResponseBody+1)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(compressed.Bytes())
	}))
	defer srv.Close()

	resp, err := getWithContext(t, NewClientFromConfig(srv.URL, "t", 1, 1).HTTPClient, srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var tooLarge *http.MaxBytesError
	if _, err := io.Copy(io.Discard, resp.Body); !errors.As(err, &tooLarge) {
		t.Fatalf("read err = %v, want the %d-byte response cap", err, maxChatwootResponseBody)
	}
}

func TestFindContactByIdentifierCapsErrorBody(t *testing.T) {
	const huge = 70 << 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("A", huge)))
	}))
	defer srv.Close()

	c := &Client{
		BaseURL:    srv.URL,
		APIToken:   "t",
		AccountID:  1,
		InboxID:    1,
		HTTPClient: srv.Client(),
	}
	_, err := c.FindContactByIdentifier("628123456789", false)
	if err == nil {
		t.Fatal("expected search error")
	}
	if strings.Contains(err.Error(), strings.Repeat("A", 1024)) {
		t.Fatal("error included a large slice of the response body")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want the size cap", err)
	}
}
