package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxUntrustedRedirects = 10

// untrustedFetchHooks is the test seam for caller-selected preview and document
// fetches. Production leaves it zero, which resolves through the default
// resolver and refuses non-public addresses. Operator-configured Chatwoot base
// URLs do not use this client.
type untrustedFetchHooks struct {
	lookup func(context.Context, string) ([]net.IP, error)
	dial   func(context.Context, string, string) (net.Conn, error)
	skip   bool
}

var (
	untrustedFetchMu sync.Mutex
	untrustedFetch   untrustedFetchHooks
)

func snapshotUntrustedFetchHooks() untrustedFetchHooks {
	untrustedFetchMu.Lock()
	defer untrustedFetchMu.Unlock()
	return untrustedFetch
}

// SetUntrustedFetchTestHooks overrides DNS lookup and dialing for caller-selected
// preview and document fetches. A nil lookup or dial keeps the current function.
// skipAddressCheck lets tests reach httptest loopback servers. Production code
// must not call this.
func SetUntrustedFetchTestHooks(
	lookup func(context.Context, string) ([]net.IP, error),
	dial func(context.Context, string, string) (net.Conn, error),
	skipAddressCheck bool,
) (restore func()) {
	untrustedFetchMu.Lock()
	prev := untrustedFetch
	next := prev
	if lookup != nil {
		next.lookup = lookup
	}
	if dial != nil {
		next.dial = dial
	}
	next.skip = skipAddressCheck
	untrustedFetch = next
	untrustedFetchMu.Unlock()
	return func() {
		untrustedFetchMu.Lock()
		untrustedFetch = prev
		untrustedFetchMu.Unlock()
	}
}

// AllowUntrustedLoopbackForTest disables the public-address check so tests can
// use httptest servers. Production defaults are unchanged when the returned
// function runs.
func AllowUntrustedLoopbackForTest() (restore func()) {
	return SetUntrustedFetchTestHooks(nil, nil, true)
}

func newUntrustedHTTPClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		// A proxy would hide the address DialContext checks.
		Proxy:                 nil,
		DialContext:           dialUntrusted,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    false,
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     transport,
		CheckRedirect: untrustedCheckRedirect,
	}
}

func doUntrusted(client *http.Client, req *http.Request) (*http.Response, error) {
	if err := validateUntrustedURL(req.URL.String()); err != nil {
		return nil, err
	}
	return client.Do(req)
}

func untrustedCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxUntrustedRedirects {
		return fmt.Errorf("too many redirects")
	}
	return validateUntrustedURL(req.URL.String())
}

func validateUntrustedURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported protocol scheme %q", parsed.Scheme)
	}
	host := normalizeFetchHost(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("missing host")
	}
	hooks := snapshotUntrustedFetchHooks()
	if hooks.skip {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := resolveUntrustedHost(ctx, host, hooks)
	if err != nil {
		return err
	}
	return rejectBlockedIPs(host, ips)
}

func normalizeFetchHost(host string) string {
	host = strings.TrimSpace(host)
	if zone := strings.IndexByte(host, '%'); zone >= 0 {
		host = host[:zone]
	}
	return host
}

func resolveUntrustedHost(ctx context.Context, host string, hooks untrustedFetchHooks) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	if hooks.lookup != nil {
		ips, err := hooks.lookup(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no addresses for host %s", host)
		}
		return ips, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if addr.IP != nil {
			ips = append(ips, addr.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses for host %s", host)
	}
	return ips, nil
}

func rejectBlockedIPs(host string, ips []net.IP) error {
	if len(ips) == 0 {
		return fmt.Errorf("no addresses for host %s", host)
	}
	for _, ip := range ips {
		if isBlockedFetchIP(ip) {
			return fmt.Errorf("refusing to fetch non-public address %s", ip)
		}
	}
	return nil
}

func isBlockedFetchIP(ip net.IP) bool {
	if len(ip) == 0 || ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 255 && v4[1] == 255 && v4[2] == 255 && v4[3] == 255 {
		return true
	}
	return false
}

// dialUntrusted resolves the dial address, refuses non-public results, and
// connects to the checked IP. Re-checking here covers DNS rebinding between
// the request check and the connection.
func dialUntrusted(ctx context.Context, network, address string) (net.Conn, error) {
	hooks := snapshotUntrustedFetchHooks()
	if hooks.skip {
		if hooks.dial != nil {
			return hooks.dial(ctx, network, address)
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	host = normalizeFetchHost(host)
	ips, err := resolveUntrustedHost(ctx, host, hooks)
	if err != nil {
		return nil, err
	}
	if err := rejectBlockedIPs(host, ips); err != nil {
		return nil, err
	}
	if hooks.dial != nil {
		return hooks.dial(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	var dialer net.Dialer
	dialer.Timeout = 10 * time.Second
	var last error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no route to %s", address)
	}
	return nil, last
}
