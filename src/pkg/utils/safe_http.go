package utils

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"golang.org/x/net/idna"
)

const maxUntrustedRedirects = 10

var errNonPublicAddress = errors.New("refusing to fetch non-public address")

// untrustedFetchHooks is the test seam for caller-selected preview, document,
// and media fetches. Production leaves it zero, which resolves through the
// default resolver and refuses non-public addresses. Operator-configured
// Chatwoot base URLs do not use this client.
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
// preview, document, and media fetches. A nil lookup or dial keeps the current function.
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

// untrustedTransport is shared so keep-alive connections are pooled and expire.
// It keeps the default HTTP/2 and idle-connection settings, replaces dialing,
// and connects directly: a proxy would resolve the destination again after it
// was checked, so HTTP(S)_PROXY is not used for these fetches.
var untrustedTransport = func() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = dialUntrusted
	return transport
}()

type chatwootAttachmentFetchKey struct{}

// WithChatwootAttachmentFetch marks ctx as fetching a Chatwoot attachment. Only
// such fetches may reach the operator-configured Chatwoot hosts, which can be
// private; every other caller-selected URL keeps the public-address check.
func WithChatwootAttachmentFetch(ctx context.Context) context.Context {
	return context.WithValue(ctx, chatwootAttachmentFetchKey{}, true)
}

func newUntrustedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     untrustedTransport,
		CheckRedirect: untrustedCheckRedirect,
	}
}

func doUntrusted(client *http.Client, req *http.Request) (*http.Response, error) {
	if err := validateUntrustedURL(req.Context(), req.URL.String()); err != nil {
		// gowa's own file URLs (from /message/:id/download or webhook media
		// paths) point at a loopback or private address in default deployments.
		// Serve those from disk; /statics is already public, so nothing new is
		// exposed.
		if errors.Is(err, errNonPublicAddress) {
			if resp, ok := localStaticResponse(req); ok {
				return resp, nil
			}
		}
		return nil, err
	}
	return client.Do(req)
}

// localStaticResponse answers a GET for a gowa /statics URL from the stored
// file. os.Root keeps the lookup inside the statics directory, whatever
// separators or symlinks the path holds.
func localStaticResponse(req *http.Request) (*http.Response, bool) {
	rel, ok := strings.CutPrefix(req.URL.Path, config.AppBasePath+"/statics/")
	if !ok || req.Method != http.MethodGet {
		return nil, false
	}
	root, err := os.OpenRoot("statics")
	if err != nil {
		return nil, false
	}
	defer root.Close()
	file, err := root.Open(filepath.FromSlash(path.Clean(rel)))
	if err != nil {
		return nil, false
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, false
	}
	header := http.Header{}
	if contentType := mime.TypeByExtension(filepath.Ext(rel)); contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          file,
		ContentLength: info.Size(),
		Request:       req,
	}, true
}

func untrustedCheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxUntrustedRedirects {
		return fmt.Errorf("too many redirects")
	}
	return validateUntrustedURL(req.Context(), req.URL.String())
}

func validateUntrustedURL(ctx context.Context, raw string) error {
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
	// net/http dials the punycode form of a non-ASCII host, so resolve and
	// check that same name.
	if strings.ContainsFunc(host, func(r rune) bool { return r >= utf8.RuneSelf }) {
		if host, err = idna.Lookup.ToASCII(host); err != nil {
			return fmt.Errorf("invalid host: %w", err)
		}
	}
	hooks := snapshotUntrustedFetchHooks()
	if hooks.skip || operatorTrustedHost(ctx, host, urlPort(parsed)) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := resolveUntrustedHost(ctx, host, hooks)
	if err != nil {
		return err
	}
	return rejectBlockedIPs(host, ips)
}

// operatorTrustedHost reports whether a Chatwoot attachment fetch (see
// WithChatwootAttachmentFetch) targets the CHATWOOT_URL host and port, or a
// host listed in CHATWOOT_ALLOWED_HOSTS, matched case-insensitively as the
// chatwoot package does. Chatwoot serves attachments from these
// operator-configured hosts, which may be private, so those fetches skip the
// public-address check.
func operatorTrustedHost(ctx context.Context, host, port string) bool {
	if ctx.Value(chatwootAttachmentFetchKey{}) == nil {
		return false
	}
	if base, err := url.Parse(strings.TrimSpace(config.ChatwootURL)); err == nil && strings.EqualFold(base.Hostname(), host) && urlPort(base) == port {
		return true
	}
	for _, allowed := range config.ChatwootAllowedHosts {
		if strings.EqualFold(strings.TrimSpace(allowed), host) {
			return true
		}
	}
	return false
}

// urlPort returns u's port, defaulting to the scheme's port.
func urlPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
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
		if IsBlockedFetchIP(ip) {
			return fmt.Errorf("%w %s", errNonPublicAddress, ip)
		}
	}
	return nil
}

// IPv6 transition prefixes. NAT64 well-known, IPv4-compatible, SIIT, and 6to4
// embed an IPv4 address; local-use NAT64, Teredo, and deprecated site-local are
// refused outright.
var (
	nat64WellKnown = netip.MustParsePrefix("64:ff9b::/96")
	nat64LocalUse  = netip.MustParsePrefix("64:ff9b:1::/48")
	ipv4Compatible = netip.MustParsePrefix("::/96")
	siit           = netip.MustParsePrefix("::ffff:0:0:0/96")
	sixToFour      = netip.MustParsePrefix("2002::/16")
	teredo         = netip.MustParsePrefix("2001::/32")
	siteLocal      = netip.MustParsePrefix("fec0::/10")
)

// IsBlockedFetchIP reports whether ip is not publicly routable and must not be
// reached by a caller-selected URL. The Chatwoot per-device guard shares it.
func IsBlockedFetchIP(ip net.IP) bool {
	if len(ip) == 0 || ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// To4 also matches IPv4-mapped IPv6, so the ranges below cover both forms.
	v4 := ip.To4()
	if v4 == nil {
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			return false
		}
		b := addr.As16()
		switch {
		case nat64LocalUse.Contains(addr), teredo.Contains(addr), siteLocal.Contains(addr):
			return true
		case nat64WellKnown.Contains(addr), ipv4Compatible.Contains(addr), siit.Contains(addr):
			// These carry an IPv4 address in the last four bytes. NAT64 also
			// maps public IPv4 for DNS64, so the prefix is not refused outright.
			return IsBlockedFetchIP(net.IP(b[12:]))
		case sixToFour.Contains(addr):
			return IsBlockedFetchIP(net.IP(b[2:6]))
		default:
			return false
		}
	}
	switch {
	case v4[0] == 0:
		// 0.0.0.0/8, including "this host" and 0.0.0.1.
		return true
	case v4[0] == 100 && v4[1]&0xc0 == 64:
		// 100.64.0.0/10 carrier-grade NAT, including Alibaba metadata 100.100.100.200.
		return true
	case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
		// 192.0.0.0/24 IETF protocol assignments.
		return true
	case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19):
		// 198.18.0.0/15 network benchmark tests.
		return true
	case v4[0] >= 240:
		// 240.0.0.0/4 reserved (includes limited broadcast).
		return true
	default:
		return false
	}
}

// dialUntrusted lets net.Dialer resolve the dial address and race its IPs with
// the usual IPv6/IPv4 fallback, while rejectBlockedConnect refuses every
// non-public IP it is about to connect to. Checking each connect covers DNS
// rebinding between the request check and the connection. Operator-trusted
// hosts are dialed as given. Test lookup and dial hooks resolve, check, and
// then dial the checked IP.
func dialUntrusted(ctx context.Context, network, address string) (net.Conn, error) {
	hooks := snapshotUntrustedFetchHooks()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	host = normalizeFetchHost(host)
	if hooks.skip || operatorTrustedHost(ctx, host, port) {
		if hooks.dial != nil {
			return hooks.dial(ctx, network, address)
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	}
	if hooks.lookup == nil && hooks.dial == nil {
		dialer := net.Dialer{Timeout: 10 * time.Second, Control: rejectBlockedConnect}
		return dialer.DialContext(ctx, network, address)
	}
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

// rejectBlockedConnect is the net.Dialer Control for untrusted fetches. It
// receives each resolved IP and port just before the connect.
func rejectBlockedConnect(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if IsBlockedFetchIP(net.ParseIP(host)) {
		return fmt.Errorf("%w %s", errNonPublicAddress, host)
	}
	return nil
}
