package chatwoot

import (
	"fmt"
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
	guarded := NewClientFromConfig(srv.URL, "t", 1, 1)
	if _, err := guarded.HTTPClient.Get(srv.URL); err == nil {
		t.Fatal("guarded client connected to a loopback address, want block")
	} else if !strings.Contains(err.Error(), "disallowed address") {
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
	resp, err := allowed.HTTPClient.Get(okSrv.URL)
	if err != nil {
		t.Fatalf("allowlisted loopback should connect: %v", err)
	}
	resp.Body.Close()

	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example/steal", http.StatusFound)
	}))
	defer redirectSrv.Close()

	redirected := NewClientFromConfig(redirectSrv.URL, "t", 1, 1)
	if _, err := redirected.HTTPClient.Get(redirectSrv.URL + "/v1"); err == nil || !strings.Contains(err.Error(), "evil.example") {
		t.Fatalf("redirect off the allowlist: err=%v", err)
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
