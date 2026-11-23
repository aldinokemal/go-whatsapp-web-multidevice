package oauth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The test documents live on "example.com" (a name httptest's certificate
// covers); the client's dialer sends every connection to the test server.
const (
	cimdHost        = "example.com"
	desktopClientID = "https://example.com/oauth/mcp-oauth-client-metadata"
	codeClientID    = "https://example.com/oauth/claude-code-client-metadata"
)

type docServer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	docs     map[string]func(w http.ResponseWriter, r *http.Request)
	requests atomic.Int64
	methods  []string
	bodyRead atomic.Int64 // bytes our client actually read from response bodies
}

func desktopDoc(clientID string, redirects ...string) map[string]any {
	return map[string]any{
		"client_id": clientID, "client_name": "Claude", "redirect_uris": redirects,
		"grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
		"token_endpoint_auth_method": "none", "scope": "mcp:use offline_access",
	}
}

func newDocServer(t *testing.T) *docServer {
	t.Helper()
	d := &docServer{docs: map[string]func(http.ResponseWriter, *http.Request){}}
	d.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.requests.Add(1)
		d.mu.Lock()
		d.methods = append(d.methods, r.Method)
		h := d.docs[r.URL.Path]
		d.mu.Unlock()
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed) // like Claude's endpoints
			return
		}
		if h == nil {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(d.srv.Close)
	return d
}

func (d *docServer) serveJSON(path string, doc any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.docs[path] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(doc)
	}
}

func (d *docServer) handle(path string, h func(http.ResponseWriter, *http.Request)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.docs[path] = h
}

type countingBody struct {
	io.ReadCloser
	n *atomic.Int64
}

func (c countingBody) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.n.Add(int64(n))
	return n, err
}

type countingTransport struct {
	base http.RoundTripper
	n    *atomic.Int64
}

func (c countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := c.base.RoundTrip(r)
	if resp != nil {
		resp.Body = countingBody{resp.Body, c.n}
	}
	return resp, err
}

// client reaches the test server whatever host a URL names.
func (d *docServer) client() *http.Client {
	base := d.srv.Client()
	tr := base.Transport.(*http.Transport).Clone()
	addr := d.srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: countingTransport{tr, &d.bodyRead}, Timeout: 5 * time.Second}
}

func newCIMDTestServer(t *testing.T, d *docServer, mod func(*CIMDConfig)) (*Server, *fiber.App, *fakeSession, string) {
	t.Helper()
	cfg := &CIMDConfig{AllowedHosts: []string{cimdHost}, HTTPClient: d.client()}
	if mod != nil {
		mod(cfg)
	}
	return newSessionTestServer(t, Config{DisableDCR: true, CIMD: cfg})
}

func cimdQuery(clientID, redirect string) (url.Values, string) {
	verifier := strings.Repeat("d", 43)
	sum := sha256.Sum256([]byte(verifier))
	return url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"resource": {testResource}, "scope": {"mcp"}, "state": {"st"},
	}, verifier
}

func getAuthorize(t *testing.T, app *fiber.App, q url.Values) *http.Response {
	t.Helper()
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	req.Header.Set("X-Test-User", "u_alice")
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func countClients(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, "file:"+filepath.ToSlash(dbPath))
	require.NoError(t, err)
	defer db.Close()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM oauth_clients`).Scan(&n))
	return n
}

func TestClaudeDesktopDocumentAccepted(t *testing.T) {
	d := newDocServer(t)
	d.serveJSON("/oauth/mcp-oauth-client-metadata", desktopDoc(desktopClientID, testRedirect))
	srv, app, fake, _ := newCIMDTestServer(t, d, nil)

	q, verifier := cimdQuery(desktopClientID, testRedirect)
	resp := getAuthorize(t, app, q)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, cimdHost, fake.lastView.ClientHost)
	assert.Equal(t, "Claude", fake.lastView.ClientName)
	assert.False(t, fake.lastView.LoopbackOnly)
	assert.Equal(t, "https://claude.ai", fake.origin)

	rec := consentPost(t, app, q, "u_alice", "approve", fakeCSRF)
	require.Equal(t, fiber.StatusFound, rec.Code, rec.Body.String())
	cb, _ := url.Parse(rec.Header().Get("Location"))
	tokResp := postForm(t, app, "/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {desktopClientID}, "code": {cb.Query().Get("code")},
		"redirect_uri": {testRedirect}, "code_verifier": {verifier}, "resource": {testResource},
	})
	require.Equal(t, fiber.StatusOK, tokResp.StatusCode)
	var pair tokenResponse
	require.NoError(t, json.NewDecoder(tokResp.Body).Decode(&pair))
	p, err := srv.ValidateAccessToken(context.Background(), pair.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, desktopClientID, p.ClientID)

	grants, err := srv.ListGrants(context.Background(), "u_alice")
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, cimdHost, grants[0].ClientHost)
	assert.Equal(t, "Claude", grants[0].ClientName)

	refresh := postForm(t, app, "/oauth/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {desktopClientID}, "refresh_token": {pair.RefreshToken}, "resource": {testResource},
	})
	assert.Equal(t, fiber.StatusOK, refresh.StatusCode)
}

func TestClaudeCodeEphemeralPortAccepted(t *testing.T) {
	d := newDocServer(t)
	d.serveJSON("/oauth/claude-code-client-metadata", desktopDoc(codeClientID, "http://localhost/callback", "http://127.0.0.1/callback"))
	_, app, fake, _ := newCIMDTestServer(t, d, nil)

	for _, redirect := range []string{"http://localhost:54321/callback", "http://127.0.0.1:61000/callback", "http://localhost/callback"} {
		q, _ := cimdQuery(codeClientID, redirect)
		resp := getAuthorize(t, app, q)
		require.Equal(t, fiber.StatusOK, resp.StatusCode, redirect)
		u, _ := url.Parse(redirect)
		assert.Equal(t, "http://"+u.Host, fake.origin, "form-action gets the requested port")
		assert.True(t, fake.lastView.LoopbackOnly)
	}
	q, _ := cimdQuery(codeClientID, "http://localhost:54321/callback")
	rec := consentPost(t, app, q, "u_alice", "approve", fakeCSRF)
	require.Equal(t, fiber.StatusFound, rec.Code)
	assert.True(t, strings.HasPrefix(rec.Header().Get("Location"), "http://localhost:54321/callback?"), rec.Header().Get("Location"))
}

func TestLocalhostDoesNotMatch127(t *testing.T) {
	d := newDocServer(t)
	d.serveJSON("/oauth/claude-code-client-metadata", desktopDoc(codeClientID, "http://localhost/callback"))
	_, app, _, _ := newCIMDTestServer(t, d, nil)
	for _, redirect := range []string{"http://127.0.0.1:5555/callback", "http://[::1]:5555/callback", "http://localhost:5555/other",
		"https://localhost:5555/callback", "http://localhost.evil.example:5555/callback"} {
		q, _ := cimdQuery(codeClientID, redirect)
		resp := getAuthorize(t, app, q)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode, redirect)
		assert.Empty(t, resp.Header.Get("Location"), "an unmatched redirect is never followed")
	}
}

func TestQueryOrderDoesNotMatterButExtrasDo(t *testing.T) {
	declared := []string{"https://claude.ai/cb?a=1&b=2"}
	assert.True(t, cimdRedirectMatches(declared, "https://claude.ai/cb?b=2&a=1"))
	assert.False(t, cimdRedirectMatches(declared, "https://claude.ai/cb?a=1&b=2&c=3"))
	assert.False(t, cimdRedirectMatches(declared, "https://claude.ai/cb?a=1"))
	assert.False(t, cimdRedirectMatches(declared, "https://claude.ai/cb?a=1&b=3"))
	assert.False(t, cimdRedirectMatches([]string{"https://claude.ai/cb"}, "https://claude.ai/cb?evil=1"))
	assert.False(t, cimdRedirectMatches([]string{"https://claude.ai/cb"}, "https://claude.ai/cb#x"))
	assert.False(t, cimdRedirectMatches([]string{"https://claude.ai/cb"}, "https://user@claude.ai/cb"))
}

func TestDefaultPortEquivalence(t *testing.T) {
	assert.True(t, cimdRedirectMatches([]string{"https://x.example/cb"}, "https://x.example:443/cb"))
	assert.True(t, cimdRedirectMatches([]string{"https://x.example:443/cb"}, "https://x.example/cb"))
	assert.False(t, cimdRedirectMatches([]string{"https://x.example/cb"}, "https://x.example:8443/cb"))
	assert.False(t, cimdRedirectMatches([]string{"https://claude.ai/cb"}, "http://claude.ai/cb"))
}

func TestHostNotAllowlistedRefusedWithoutFetching(t *testing.T) {
	d := newDocServer(t)
	_, app, _, _ := newCIMDTestServer(t, d, nil)
	for _, id := range []string{"https://evil.example/oauth/meta", "https://example.com.evil.example/x", "https://example.com:8443/x",
		"https://user@example.com/x", "https://example.com/", "https://example.com", "https://example.com/a/../x", "http://example.com/x"} {
		q, _ := cimdQuery(id, testRedirect)
		resp := getAuthorize(t, app, q)
		assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode, id)
		assert.Empty(t, resp.Header.Get("Location"))
	}
	assert.Zero(t, d.requests.Load(), "nothing is fetched for a refused client_id")
}

func TestSelfReferenceRequired(t *testing.T) {
	d := newDocServer(t)
	d.serveJSON("/oauth/mcp-oauth-client-metadata", desktopDoc("https://example.com/somebody-else", testRedirect))
	_, app, _, _ := newCIMDTestServer(t, d, nil)
	q, _ := cimdQuery(desktopClientID, testRedirect)
	resp := getAuthorize(t, app, q)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
}

func TestDocumentRulesEnforced(t *testing.T) {
	cases := map[string]map[string]any{
		"secret client":  {"client_id": desktopClientID, "redirect_uris": []string{testRedirect}, "token_endpoint_auth_method": "client_secret_basic"},
		"no redirects":   {"client_id": desktopClientID},
		"http redirect":  {"client_id": desktopClientID, "redirect_uris": []string{"http://claude.ai/cb"}},
		"implicit grant": {"client_id": desktopClientID, "redirect_uris": []string{testRedirect}, "grant_types": []string{"implicit"}},
	}
	for name, doc := range cases {
		d := newDocServer(t)
		d.serveJSON("/oauth/mcp-oauth-client-metadata", doc)
		_, app, _, _ := newCIMDTestServer(t, d, nil)
		q, _ := cimdQuery(desktopClientID, testRedirect)
		assert.Equal(t, fiber.StatusBadRequest, getAuthorize(t, app, q).StatusCode, name)
	}
}

func TestRedirectNotFollowed(t *testing.T) {
	d := newDocServer(t)
	var targetHit atomic.Bool
	d.handle("/oauth/mcp-oauth-client-metadata", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	d.handle("/elsewhere", func(w http.ResponseWriter, _ *http.Request) {
		targetHit.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(desktopDoc(desktopClientID, testRedirect))
	})
	_, app, _, _ := newCIMDTestServer(t, d, nil)
	q, _ := cimdQuery(desktopClientID, testRedirect)
	assert.Equal(t, fiber.StatusBadRequest, getAuthorize(t, app, q).StatusCode)
	assert.False(t, targetHit.Load(), "the redirect target is never fetched")
}

func TestOversizeDocumentAbandonedMidStream(t *testing.T) {
	d := newDocServer(t)
	d.handle("/oauth/mcp-oauth-client-metadata", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		chunk := []byte(strings.Repeat(" ", 4096))
		_, _ = w.Write([]byte(`{"client_id":"` + desktopClientID + `","padding":"`))
		for i := 0; i < 256; i++ { // 1 MiB
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})
	const maxBytes = 4096
	_, app, _, _ := newCIMDTestServer(t, d, func(c *CIMDConfig) { c.MaxBytes = maxBytes })
	q, _ := cimdQuery(desktopClientID, testRedirect)
	assert.Equal(t, fiber.StatusBadRequest, getAuthorize(t, app, q).StatusCode)
	assert.LessOrEqual(t, d.bodyRead.Load(), int64(maxBytes+1), "the body is abandoned at the cap, not read and then measured")
}

func TestHTMLContentTypeRefused(t *testing.T) {
	d := newDocServer(t)
	d.handle("/oauth/mcp-oauth-client-metadata", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_ = json.NewEncoder(w).Encode(desktopDoc(desktopClientID, testRedirect))
	})
	_, app, _, _ := newCIMDTestServer(t, d, nil)
	q, _ := cimdQuery(desktopClientID, testRedirect)
	assert.Equal(t, fiber.StatusBadRequest, getAuthorize(t, app, q).StatusCode)
}

func TestHeadIsNeverUsed(t *testing.T) {
	d := newDocServer(t)
	d.serveJSON("/oauth/mcp-oauth-client-metadata", desktopDoc(desktopClientID, testRedirect))
	_, app, _, _ := newCIMDTestServer(t, d, nil)
	q, _ := cimdQuery(desktopClientID, testRedirect)
	require.Equal(t, fiber.StatusOK, getAuthorize(t, app, q).StatusCode)
	d.mu.Lock()
	defer d.mu.Unlock()
	assert.Equal(t, []string{"GET"}, d.methods)
}

func TestUnknownClientIsNotAReachableStateForCIMD(t *testing.T) {
	d := newDocServer(t)
	d.serveJSON("/oauth/mcp-oauth-client-metadata", desktopDoc(desktopClientID, testRedirect))
	_, app, _, dbPath := newCIMDTestServer(t, d, nil)
	require.Zero(t, countClients(t, dbPath), "nothing registered beforehand")
	q, _ := cimdQuery(desktopClientID, testRedirect)
	require.Equal(t, fiber.StatusOK, getAuthorize(t, app, q).StatusCode, "a CIMD client needs no row to be authorized")
	assert.Zero(t, countClients(t, dbPath), "showing consent writes nothing")
}

func TestRegisteredClientsRefusedWhenDCRIsOff(t *testing.T) {
	d := newDocServer(t)
	_, app, _, _ := newCIMDTestServer(t, d, nil)
	q, _ := cimdQuery("gowa_client_whatever", testRedirect)
	assert.Equal(t, fiber.StatusBadRequest, getAuthorize(t, app, q).StatusCode)
	resp := postForm(t, app, "/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {"gowa_client_x"},
		"refresh_token": {"x"}, "resource": {testResource}})
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
}

func TestMetadataAdvertisesCIMD(t *testing.T) {
	d := newDocServer(t)
	_, app, _, _ := newCIMDTestServer(t, d, nil)
	resp, err := app.Test(httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil))
	require.NoError(t, err)
	var md map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&md))
	assert.Equal(t, true, md["client_id_metadata_document_supported"])
	assert.Contains(t, md["token_endpoint_auth_methods_supported"], "none")
	assert.Equal(t, []any{"S256"}, md["code_challenge_methods_supported"])
	_, hasReg := md["registration_endpoint"]
	assert.False(t, hasReg)
}

func TestCacheServesWithinTTLAndRefetchesAfter(t *testing.T) {
	d := newDocServer(t)
	d.serveJSON("/oauth/mcp-oauth-client-metadata", desktopDoc(desktopClientID, testRedirect))
	srv, app, _, _ := newCIMDTestServer(t, d, func(c *CIMDConfig) { c.CacheTTL = time.Minute })
	clock := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	srv.now = func() time.Time { return clock }
	q, _ := cimdQuery(desktopClientID, testRedirect)
	require.Equal(t, fiber.StatusOK, getAuthorize(t, app, q).StatusCode)
	require.Equal(t, fiber.StatusOK, getAuthorize(t, app, q).StatusCode)
	assert.EqualValues(t, 1, d.requests.Load(), "second request within the TTL is served from the cache")
	clock = clock.Add(61 * time.Second)
	require.Equal(t, fiber.StatusOK, getAuthorize(t, app, q).StatusCode)
	assert.EqualValues(t, 2, d.requests.Load(), "after the TTL the document is fetched again")
}

func TestFailuresAreNotCached(t *testing.T) {
	d := newDocServer(t)
	d.serveJSON("/oauth/mcp-oauth-client-metadata", desktopDoc("https://example.com/wrong", testRedirect))
	_, app, _, _ := newCIMDTestServer(t, d, nil)
	q, _ := cimdQuery(desktopClientID, testRedirect)
	require.Equal(t, fiber.StatusBadRequest, getAuthorize(t, app, q).StatusCode)
	d.serveJSON("/oauth/mcp-oauth-client-metadata", desktopDoc(desktopClientID, testRedirect))
	assert.Equal(t, fiber.StatusOK, getAuthorize(t, app, q).StatusCode, "a fixed document is picked up at once")
}

func TestNewRefusesCIMDWithoutAClient(t *testing.T) {
	_, err := New(Config{IssuerURL: testIssuer, ResourceURL: testResource, StorageURI: "file:" + filepath.Join(t.TempDir(), "o.db"),
		Session: &fakeSession{}, CIMD: &CIMDConfig{AllowedHosts: []string{"claude.ai"}}}, nil)
	require.Error(t, err)
}
