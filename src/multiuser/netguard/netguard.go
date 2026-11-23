// Package netguard keeps server-side fetches that a user can steer (send by
// URL, link previews, per-device webhooks, OAuth client metadata) away from
// anything but the public internet.
//
// The real guard is the dialer: its Control hook judges the exact address
// each connection is about to use, after DNS resolution, so a host that
// answers with a public address once and a private one later (DNS rebinding)
// is still refused, and so is every redirect hop. CheckURL is an advisory
// check for save time; it gives early, friendly errors but is not relied on.
package netguard

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrRefused is wrapped by every refusal this package makes.
var ErrRefused = errors.New("outbound request refused")

func refuse(reason string) error { return fmt.Errorf("%w: %s", ErrRefused, reason) }

const (
	reasonScheme    = "not https or http"
	reasonAddress   = "address not public"
	reasonRedirects = "too many redirects"
	reasonBody      = "body too large"
)

// testAllow lets in-package tests treat one local address as public so an
// httptest server can stand in for a remote host. It is unexported and never
// set outside _test.go files, so no config or caller can reach it.
var testAllow func(netip.Addr) bool

// refusedPrefixes are the special-purpose ranges (IANA IPv4 and IPv6
// special-purpose registries) that are not the public internet.
var refusedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		// IPv4
		"0.0.0.0/8",       // "this network", unspecified
		"10.0.0.0/8",      // private
		"100.64.0.0/10",   // CGNAT shared address space
		"127.0.0.0/8",     // loopback
		"169.254.0.0/16",  // link-local, cloud metadata (169.254.169.254)
		"172.16.0.0/12",   // private
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // documentation
		"192.88.99.0/24",  // 6to4 relay anycast (deprecated)
		"192.168.0.0/16",  // private
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
		"224.0.0.0/4",     // multicast
		"240.0.0.0/4",     // reserved, includes broadcast
		// IPv6
		"::/96",          // unspecified, loopback, deprecated IPv4-compatible
		"64:ff9b:1::/48", // local-use NAT64
		"100::/64",       // discard
		"2001::/23",      // IETF protocol assignments, includes Teredo 2001::/32
		"2001:db8::/32",  // documentation
		"2002::/16",      // 6to4, embeds an IPv4 address
		"3fff::/20",      // documentation
		"5f00::/16",      // segment routing SIDs
		"fc00::/7",       // unique local
		"fe80::/10",      // link-local
		"fec0::/10",      // site-local (deprecated)
		"ff00::/8",       // multicast
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

var nat64 = netip.MustParsePrefix("64:ff9b::/96")

// Allowed reports whether ip is a globally routable unicast address.
// IPv4-mapped IPv6 and NAT64 (64:ff9b::/96) addresses are judged by the IPv4
// address they carry.
func Allowed(ip netip.Addr) bool {
	if testAllow != nil && testAllow(ip.WithZone("").Unmap()) {
		return true
	}
	return allowed(ip)
}

func allowed(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	ip = ip.WithZone("").Unmap()
	if ip.Is6() && nat64.Contains(ip) {
		b := ip.As16()
		return allowed(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range refusedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// control runs after DNS resolution and immediately before connect(2), with
// the literal address being dialled. Judging this address, rather than any
// earlier DNS answer, is what pins the check to the real destination.
func control(network, address string, _ syscall.RawConn) error {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return refuse("network " + network)
	}
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !Allowed(ap.Addr()) {
		return refuse(reasonAddress)
	}
	return nil
}

// Dialer returns a dialer that refuses any connection to a non-public address.
func Dialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   control,
	}
}

// Options configures NewClient.
type Options struct {
	// Timeout bounds the whole exchange, including reading the body.
	Timeout time.Duration
	// MaxRedirects is the number of redirects followed; 0 follows none.
	MaxRedirects int
	// MaxBody caps every response body; 0 means no cap beyond the caller's.
	MaxBody int64
	// InsecureSkipVerify skips TLS certificate checks (per-device webhook
	// option). It does not weaken the address checks.
	InsecureSkipVerify bool
}

func newTransport(insecureSkipVerify bool) *http.Transport {
	t := &http.Transport{
		Proxy:                 nil, // a proxy would hide the real destination from the dialer
		DialContext:           Dialer().DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if insecureSkipVerify {
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // per-device opt-in, address checks still apply
	}
	return t
}

// NewClient returns an HTTP client whose every connection, including each
// redirect hop, goes through the guarded dialer.
func NewClient(o Options) *http.Client {
	var rt http.RoundTripper = newTransport(o.InsecureSkipVerify)
	if o.MaxBody > 0 {
		rt = &bodyCap{next: rt, max: o.MaxBody}
	}
	max := o.MaxRedirects
	return &http.Client{
		Timeout:   o.Timeout,
		Transport: rt,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > max {
				return refuse(reasonRedirects)
			}
			if s := req.URL.Scheme; s != "http" && s != "https" {
				return refuse(reasonScheme)
			}
			return nil
		},
	}
}

// Client is NewClient with a timeout and a redirect cap and no body cap.
func Client(timeout time.Duration, maxRedirects int) *http.Client {
	return NewClient(Options{Timeout: timeout, MaxRedirects: maxRedirects})
}

// lookup resolves a host for CheckURL; tests replace it.
var lookup = defaultLookup

func defaultLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// CheckURL is the save-time check: http or https, a host, and every address
// the host resolves to is public. It is advisory; the dialer is the guard.
func CheckURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return refuse("not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return refuse(reasonScheme)
	}
	host := u.Hostname()
	if host == "" {
		return refuse("no host")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !Allowed(ip) {
			return refuse(reasonAddress)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addrs, err := lookup(ctx, host)
	if err != nil || len(addrs) == 0 {
		return refuse("host does not resolve")
	}
	for _, a := range addrs {
		if !Allowed(a) {
			return refuse(reasonAddress)
		}
	}
	return nil
}

// LimitBody returns a reader that yields at most max bytes of r and then
// fails with ErrRefused if r has more. It never pulls more than max+1 bytes.
func LimitBody(r io.Reader, max int64) io.Reader {
	return &limitedReader{r: r, left: max}
}

type limitedReader struct {
	r    io.Reader
	left int64 // bytes still allowed; negative once the cap is passed
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left < 0 {
		return 0, refuse(reasonBody)
	}
	if int64(len(p)) > l.left+1 {
		p = p[:l.left+1]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	if l.left < 0 {
		return n + int(l.left), refuse(reasonBody)
	}
	return n, err
}

type limitedBody struct {
	io.Reader
	io.Closer
}

// bodyCap applies LimitBody to every response and refuses a declared
// Content-Length over the cap before any of it is read.
type bodyCap struct {
	next http.RoundTripper
	max  int64
}

func (b *bodyCap) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := b.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > b.max {
		resp.Body.Close()
		return nil, refuse(reasonBody)
	}
	resp.Body = limitedBody{Reader: LimitBody(resp.Body, b.max), Closer: resp.Body}
	return resp, nil
}
