package netguard

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// allowOnly lets exactly one address through the guard for the length of a
// test, so a local httptest server can stand in for a public host. It uses the
// unexported testAllow seam, which nothing outside this package can reach.
func allowOnly(t *testing.T, ip string) {
	t.Helper()
	want := netip.MustParseAddr(ip)
	testAllow = func(a netip.Addr) bool { return a == want }
	t.Cleanup(func() { testAllow = nil })
}

// Behaviour: which addresses count as public.
func TestAllowed(t *testing.T) {
	refused := []string{
		// IPv4 special ranges
		"0.0.0.0", "0.1.2.3", // unspecified, "this network"
		"127.0.0.1", "127.255.255.254", // loopback
		"10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1", // RFC 1918
		"169.254.169.254", "169.254.0.1", // link-local, cloud metadata
		"100.64.0.1", "100.127.255.254", // CGNAT
		"192.0.0.1",                                // IETF protocol assignments
		"192.0.2.1", "198.51.100.1", "203.0.113.1", // documentation
		"198.18.0.1", "198.19.255.255", // benchmarking
		"224.0.0.1", "239.255.255.250", // multicast
		"240.0.0.1", "255.255.255.255", // reserved, broadcast
		// IPv6 special ranges
		"::", "::1", // unspecified, loopback
		"fe80::1", "fe80::1%eth0", // link-local
		"fc00::1", "fd12:3456::1", // unique local
		"fec0::1",              // site-local (deprecated)
		"ff02::1", "ff05::1:3", // multicast
		"2001:db8::1",    // documentation
		"100::1",         // discard
		"2002:7f00:1::1", // 6to4 (embeds an IPv4 address)
		"2001::1",        // Teredo (embeds an IPv4 address)
		"::127.0.0.1",    // IPv4-compatible (deprecated)
		"64:ff9b:1::1",   // local-use NAT64
		// IPv4-mapped forms of refused v4
		"::ffff:127.0.0.1", "::ffff:169.254.169.254", "::ffff:10.0.0.1", "::ffff:100.64.0.1",
		// NAT64 well-known prefix mapping a refused v4
		"64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", "64:ff9b::a00:1",
	}
	allowed := []string{
		"8.8.8.8", "1.1.1.1", "93.184.215.14",
		"2606:4700::1111", "2001:4860:4860::8888",
		"::ffff:8.8.8.8",   // mapped public v4
		"64:ff9b::808:808", // NAT64 of 8.8.8.8
	}
	for _, s := range refused {
		if Allowed(netip.MustParseAddr(s)) {
			t.Errorf("Allowed(%s) = true, want false", s)
		}
	}
	for _, s := range allowed {
		if !Allowed(netip.MustParseAddr(s)) {
			t.Errorf("Allowed(%s) = false, want true", s)
		}
	}
	if Allowed(netip.Addr{}) {
		t.Error("the zero Addr must be refused")
	}
}

// Behaviour: the save-time check refuses schemes other than http and https.
func TestCheckURLRefusesNonHTTP(t *testing.T) {
	for _, raw := range []string{
		"file:///etc/passwd", "gopher://example.com/", "ftp://example.com/x",
		"javascript:alert(1)", "//example.com/x", "example.com", "",
	} {
		if err := CheckURL(raw); !errors.Is(err, ErrRefused) {
			t.Errorf("CheckURL(%q) = %v, want ErrRefused", raw, err)
		}
	}
}

// Behaviour: the save-time check refuses literal and resolved non-public hosts.
func TestCheckURLRefusesNonPublicHosts(t *testing.T) {
	lookup = func(_ context.Context, host string) ([]netip.Addr, error) {
		switch host {
		case "internal.example":
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.1.2.3")}, nil
		case "public.example":
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}
		return nil, errors.New("no such host")
	}
	t.Cleanup(func() { lookup = defaultLookup })

	for _, raw := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:3000/",
		"http://[::1]/",
		"http://[::ffff:127.0.0.1]/",
		"https://internal.example/hook", // one of its addresses is private
		"https://nowhere.example/hook",  // does not resolve
	} {
		if err := CheckURL(raw); !errors.Is(err, ErrRefused) {
			t.Errorf("CheckURL(%q) = %v, want ErrRefused", raw, err)
		}
	}
	if err := CheckURL("https://public.example/hook"); err != nil {
		t.Errorf("CheckURL(public) = %v, want nil", err)
	}
	if err := CheckURL("http://8.8.8.8:8080/x"); err != nil {
		t.Errorf("CheckURL(8.8.8.8) = %v, want nil", err)
	}
}

// Mechanism: the guard sits on the dial path. A plain loopback server is
// refused when the client connects, not by some check beside it.
func TestClientRefusesLoopbackAtDial(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hit = true }))
	defer srv.Close()

	_, err := Client(5*time.Second, 3).Get(srv.URL)
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("Get(%s) err = %v, want ErrRefused", srv.URL, err)
	}
	if hit {
		t.Fatal("the loopback server was reached")
	}

	// Same through "localhost", which resolves to loopback.
	_, err = Client(5*time.Second, 3).Get(strings.Replace(srv.URL, "127.0.0.1", "localhost", 1))
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("Get(localhost) err = %v, want ErrRefused", err)
	}
}

// Mechanism: every redirect hop is dialled through the guard.
func TestClientRefusesRedirectToLoopback(t *testing.T) {
	allowOnly(t, "127.0.0.1") // stands in for a public host
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.2:1/", http.StatusFound)
	}))
	defer srv.Close()

	_, err := Client(5*time.Second, 3).Get(srv.URL)
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("redirect to loopback: err = %v, want ErrRefused", err)
	}
}

// Mechanism: redirects are capped and their scheme is checked on each hop.
func TestClientCapsRedirectsAndChecksScheme(t *testing.T) {
	allowOnly(t, "127.0.0.1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/file":
			w.Header().Set("Location", "file:///etc/passwd")
			w.WriteHeader(http.StatusFound)
		default:
			_, _ = io.WriteString(w, "ok")
		}
	}))
	defer srv.Close()

	_, err := Client(5*time.Second, 3).Get(srv.URL + "/loop")
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("redirect loop: err = %v, want ErrRefused too many redirects", err)
	}
	_, err = Client(5*time.Second, 3).Get(srv.URL + "/file")
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("redirect to file://: err = %v, want ErrRefused", err)
	}
	resp, err := Client(5*time.Second, 3).Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("allowed host: %v", err)
	}
	resp.Body.Close()
}

// Mechanism: DNS rebinding. The save-time check is shown a public address for
// the host, but the address actually dialled is loopback; the fetch is refused
// because the dialer judges the address it connects to, not an earlier answer.
func TestRebindingIsJudgedAtConnect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	rebound := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)

	lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14")}, nil // first answer: public
	}
	t.Cleanup(func() { lookup = defaultLookup })

	if err := CheckURL(rebound); err != nil {
		t.Fatalf("CheckURL saw a public answer and should pass, got %v", err)
	}
	// Second answer (the system resolver at dial time): 127.0.0.1.
	_, err := Client(5*time.Second, 3).Get(rebound)
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("rebound fetch err = %v, want ErrRefused", err)
	}
}

// Mechanism: the guard is not bypassed by an environment proxy.
func TestClientIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://8.8.8.8:3128")
	t.Setenv("HTTPS_PROXY", "http://8.8.8.8:3128")
	if newTransport(false).Proxy != nil {
		t.Fatal("the guarded transport must not use a proxy")
	}
}

// countingReader records how many bytes were pulled from the source.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func streamServer(total int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := make([]byte, 32*1024)
		for sent := 0; sent < total; sent += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
}

// Behaviour: the body cap stops reading mid-stream.
func TestLimitBodyStopsMidStream(t *testing.T) {
	const capBytes = 64 * 1024
	srv := streamServer(10 * 1024 * 1024)
	defer srv.Close()

	resp, err := http.Get(srv.URL) // plain client: this tests LimitBody alone
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	src := &countingReader{r: resp.Body}
	got, err := io.ReadAll(LimitBody(src, capBytes))
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused (body too large)", err)
	}
	if len(got) != capBytes {
		t.Fatalf("returned %d bytes, want exactly %d", len(got), capBytes)
	}
	if src.n > capBytes+1 {
		t.Fatalf("pulled %d bytes from the stream, want at most %d", src.n, capBytes+1)
	}

	// A body at or under the cap reads in full with no error.
	small, err := io.ReadAll(LimitBody(strings.NewReader("hello"), 5))
	if err != nil || string(small) != "hello" {
		t.Fatalf("small body: %q, %v", small, err)
	}
}

// Behaviour: the guarded client applies its own body cap to every response.
func TestClientCapsBody(t *testing.T) {
	allowOnly(t, "127.0.0.1")
	srv := streamServer(10 * 1024 * 1024)
	defer srv.Close()

	c := NewClient(Options{Timeout: 5 * time.Second, MaxRedirects: 1, MaxBody: 64 * 1024})
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if !errors.Is(err, ErrRefused) || len(got) > 64*1024 {
		t.Fatalf("read %d bytes, err %v; want ErrRefused at 64 KiB", len(got), err)
	}

	// A declared Content-Length over the cap is refused before reading.
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		_, _ = w.Write(make([]byte, 1000000))
	}))
	defer big.Close()
	_, err = c.Get(big.URL)
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("declared oversize: err = %v, want ErrRefused", err)
	}
}

// Behaviour: a slow server is cut off by the timeout.
func TestClientTimesOut(t *testing.T) {
	allowOnly(t, "127.0.0.1")
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)

	start := time.Now()
	_, err := Client(200*time.Millisecond, 1).Get(srv.URL)
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("timeout took %v", time.Since(start))
	}
}

// Mechanism: the dialer itself refuses, so any transport built on it is safe.
func TestDialerRefusesPrivateAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, err = Dialer().Dial("tcp", ln.Addr().String())
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("Dial(loopback) err = %v, want ErrRefused", err)
	}
}
