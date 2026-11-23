package oauth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSession stands in for the application's sign-in: a request is "signed
// in" when it carries the X-Test-User header, and the CSRF token is fixed.
type fakeSession struct {
	inactive atomic.Bool
	lastView ConsentView
	origin   string
}

const fakeCSRF = "csrf-token-for-test"

func (f *fakeSession) Subject(c fiber.Ctx) (string, bool) {
	u := c.Get("X-Test-User")
	return u, u != "" && !f.inactive.Load()
}
func (f *fakeSession) SignInURL(next string) string { return "/signin?next=" + url.QueryEscape(next) }
func (f *fakeSession) CSRFToken(fiber.Ctx) string   { return fakeCSRF }
func (f *fakeSession) VerifyCSRF(c fiber.Ctx) bool  { return c.FormValue("csrf") == fakeCSRF }
func (f *fakeSession) RenderConsent(c fiber.Ctx, v ConsentView, origin string) error {
	f.lastView, f.origin = v, origin
	c.Set(fiber.HeaderContentSecurityPolicy, "form-action 'self' "+origin)
	return c.SendString("consent for " + v.RedirectHost)
}
func (f *fakeSession) Active(_ context.Context, subject string) bool {
	return subject != "" && !f.inactive.Load()
}

func newSessionTestServer(t *testing.T, cfg Config) (*Server, *fiber.App, *fakeSession, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	fake := &fakeSession{}
	cfg.IssuerURL, cfg.ResourceURL = testIssuer, testResource
	cfg.StorageURI = "file:" + filepath.ToSlash(dbPath)
	cfg.Session = fake
	srv, err := New(cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, srv.Close()) })
	app := fiber.New()
	srv.RegisterPublic(app)
	return srv, app, fake, dbPath
}

func sessionAuthorizeQuery(clientID string) (url.Values, string) {
	verifier := strings.Repeat("c", 43)
	sum := sha256.Sum256([]byte(verifier))
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {testRedirect},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {testResource},
		"scope":                 {"mcp"},
		"state":                 {"st-1"},
	}, verifier
}

func consentPost(t *testing.T, app *fiber.App, q url.Values, user, decision, csrf string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	for k, v := range q {
		form[k] = v
	}
	if decision != "" {
		form.Set("decision", decision)
	}
	if csrf != "" {
		form.Set("csrf", csrf)
	}
	req := httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	for k, v := range resp.Header {
		rec.Header()[k] = v
	}
	body, _ := io.ReadAll(resp.Body)
	rec.Body.Write(body)
	return rec
}

func approveAndExchange(t *testing.T, app *fiber.App, clientID, user string) tokenResponse {
	t.Helper()
	q, verifier := sessionAuthorizeQuery(clientID)
	rec := consentPost(t, app, q, user, "approve", fakeCSRF)
	require.Equal(t, fiber.StatusFound, rec.Code, rec.Body.String())
	cb, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	code := cb.Query().Get("code")
	require.NotEmpty(t, code)
	tokens := exchangeTestCodeVerifier(t, app, clientID, code, verifier)
	return tokens
}

func exchangeTestCodeVerifier(t *testing.T, app *fiber.App, clientID, code, verifier string) tokenResponse {
	t.Helper()
	resp := postForm(t, app, "/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
		"redirect_uri": {testRedirect}, "code_verifier": {verifier}, "resource": {testResource},
	})
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	var payload tokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	return payload
}

func TestAnonymousAuthorizeRedirectsToSignIn(t *testing.T) {
	_, app, _, _ := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	q, _ := sessionAuthorizeQuery(clientID)
	resp, err := app.Test(httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusSeeOther, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/signin", loc.Path)
	next, err := url.Parse(loc.Query().Get("next"))
	require.NoError(t, err)
	assert.Equal(t, "/oauth/authorize", next.Path)
	for k := range q {
		assert.Equal(t, q.Get(k), next.Query().Get(k), "next keeps %s", k)
	}
}

func TestSignedInAuthorizeShowsConsent(t *testing.T) {
	_, app, fake, _ := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	q, _ := sessionAuthorizeQuery(clientID)
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	req.Header.Set("X-Test-User", "u_alice")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.Equal(t, "claude.ai", fake.lastView.RedirectHost)
	assert.Equal(t, "https://claude.ai", fake.origin, "form-action gets the validated redirect's origin")
	assert.Equal(t, fakeCSRF, fake.lastView.CSRF)
	assert.Equal(t, testRedirect, fake.lastView.Hidden["redirect_uri"])
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
}

func TestApproveIssuesCodeForTheSessionSubject(t *testing.T) {
	srv, app, _, _ := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	tokens := approveAndExchange(t, app, clientID, "u_alice")
	p, err := srv.ValidateAccessToken(context.Background(), tokens.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "u_alice", p.Subject)
}

func TestDenyRedirectsWithAccessDenied(t *testing.T) {
	_, app, _, _ := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	q, _ := sessionAuthorizeQuery(clientID)
	rec := consentPost(t, app, q, "u_alice", "deny", fakeCSRF)
	require.Equal(t, fiber.StatusFound, rec.Code)
	cb, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "access_denied", cb.Query().Get("error"))
	assert.Equal(t, testIssuer, cb.Query().Get("iss"))
	assert.Empty(t, cb.Query().Get("code"))
}

func countCodes(t *testing.T, dbPath string) int {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, "file:"+filepath.ToSlash(dbPath))
	require.NoError(t, err)
	defer db.Close()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM oauth_authorization_codes`).Scan(&n))
	return n
}

func TestMissingDecisionIsNotConsent(t *testing.T) {
	_, app, _, dbPath := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	q, _ := sessionAuthorizeQuery(clientID)
	for _, decision := range []string{"", "yes", "Approve"} {
		rec := consentPost(t, app, q, "u_alice", decision, fakeCSRF)
		assert.Equal(t, fiber.StatusBadRequest, rec.Code, "decision %q", decision)
		assert.Empty(t, rec.Header().Get("Location"))
	}
	assert.Zero(t, countCodes(t, dbPath), "no code may be issued without an explicit approve")
}

func TestConsentPostWithoutCSRFIs403(t *testing.T) {
	_, app, _, dbPath := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	q, _ := sessionAuthorizeQuery(clientID)
	for _, csrf := range []string{"", "wrong"} {
		rec := consentPost(t, app, q, "u_alice", "approve", csrf)
		assert.Equal(t, fiber.StatusForbidden, rec.Code)
	}
	assert.Zero(t, countCodes(t, dbPath))
}

func TestConsentPostSignedOutGoesToSignIn(t *testing.T) {
	_, app, _, dbPath := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	q, _ := sessionAuthorizeQuery(clientID)
	rec := consentPost(t, app, q, "", "approve", fakeCSRF)
	assert.Equal(t, fiber.StatusSeeOther, rec.Code)
	assert.True(t, strings.HasPrefix(rec.Header().Get("Location"), "/signin?next="))
	assert.Zero(t, countCodes(t, dbPath))
}

func TestInactiveSubjectCannotRedeemOrRefresh(t *testing.T) {
	srv, app, fake, _ := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)

	// Redeem: approve while active, then go inactive before the exchange.
	q, verifier := sessionAuthorizeQuery(clientID)
	rec := consentPost(t, app, q, "u_alice", "approve", fakeCSRF)
	require.Equal(t, fiber.StatusFound, rec.Code)
	cb, _ := url.Parse(rec.Header().Get("Location"))
	fake.inactive.Store(true)
	resp := postForm(t, app, "/oauth/token", url.Values{
		"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {cb.Query().Get("code")},
		"redirect_uri": {testRedirect}, "code_verifier": {verifier}, "resource": {testResource},
	})
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_grant")

	// Refresh and use: tokens obtained while active stop working.
	fake.inactive.Store(false)
	tokens := approveAndExchange(t, app, clientID, "u_alice")
	fake.inactive.Store(true)
	resp = postForm(t, app, "/oauth/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {tokens.RefreshToken}, "resource": {testResource},
	})
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_grant")
	_, err := srv.ValidateAccessToken(context.Background(), tokens.AccessToken)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestResourceIsRequiredInSessionMode(t *testing.T) {
	_, app, _, dbPath := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	q, _ := sessionAuthorizeQuery(clientID)
	q.Del("resource")
	rec := consentPost(t, app, q, "u_alice", "approve", fakeCSRF)
	require.Equal(t, fiber.StatusFound, rec.Code)
	cb, _ := url.Parse(rec.Header().Get("Location"))
	assert.Equal(t, "invalid_target", cb.Query().Get("error"))
	assert.Empty(t, cb.Query().Get("code"))
	assert.Zero(t, countCodes(t, dbPath))
}

func TestRevokeSubjectKillsEveryToken(t *testing.T) {
	srv, app, _, _ := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	a := approveAndExchange(t, app, clientID, "u_alice")
	b := approveAndExchange(t, app, clientID, "u_alice")
	bob := approveAndExchange(t, app, clientID, "u_bob")
	require.NoError(t, srv.RevokeSubject(context.Background(), "u_alice"))
	for _, tok := range []string{a.AccessToken, b.AccessToken} {
		_, err := srv.ValidateAccessToken(context.Background(), tok)
		assert.ErrorIs(t, err, ErrInvalidToken)
	}
	resp := postForm(t, app, "/oauth/token", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {a.RefreshToken}, "resource": {testResource},
	})
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	_, err := srv.ValidateAccessToken(context.Background(), bob.AccessToken)
	assert.NoError(t, err, "another subject is untouched")
}

func TestListGrantsIsPerSubject(t *testing.T) {
	srv, app, _, _ := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	approveAndExchange(t, app, clientID, "u_alice")
	approveAndExchange(t, app, clientID, "u_alice")
	approveAndExchange(t, app, clientID, "u_bob")
	ctx := context.Background()
	alice, err := srv.ListGrants(ctx, "u_alice")
	require.NoError(t, err)
	require.Len(t, alice, 2)
	for _, g := range alice {
		assert.Equal(t, clientID, g.ClientID)
		assert.Equal(t, "claude.ai", g.ClientHost, "a registered client is named by its redirect host")
		assert.Empty(t, g.ClientName, "a registered client's name is self-asserted and never listed")
		assert.False(t, g.CreatedAt.IsZero())
	}
	bob, err := srv.ListGrants(ctx, "u_bob")
	require.NoError(t, err)
	require.Len(t, bob, 1)
	assert.NotEqual(t, alice[0].FamilyID, bob[0].FamilyID)

	require.NoError(t, srv.RevokeGrant(ctx, "u_alice", alice[0].FamilyID))
	after, err := srv.ListGrants(ctx, "u_alice")
	require.NoError(t, err)
	assert.Len(t, after, 1, "a revoked family is no longer listed")
}

func TestRevokeAnotherSubjectsGrantFails(t *testing.T) {
	srv, app, _, _ := newSessionTestServer(t, Config{})
	clientID := registerTestClient(t, app)
	bobTokens := approveAndExchange(t, app, clientID, "u_bob")
	ctx := context.Background()
	bob, err := srv.ListGrants(ctx, "u_bob")
	require.NoError(t, err)
	require.Len(t, bob, 1)
	assert.ErrorIs(t, srv.RevokeGrant(ctx, "u_alice", bob[0].FamilyID), ErrInvalidGrant)
	assert.ErrorIs(t, srv.RevokeGrant(ctx, "u_alice", "gowa_family_nope"), ErrInvalidGrant)
	_, err = srv.ValidateAccessToken(ctx, bobTokens.AccessToken)
	assert.NoError(t, err, "Bob's connection survives Alice's attempt")
}

func TestDCRCanBeDisabled(t *testing.T) {
	_, app, _, _ := newSessionTestServer(t, Config{DisableDCR: true})
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(`{"redirect_uris":["https://claude.ai/cb"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)

	resp, err = app.Test(httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil))
	require.NoError(t, err)
	var md map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&md))
	_, has := md["registration_endpoint"]
	assert.False(t, has)
}

func TestNewRefusesNoValidatorWithoutSession(t *testing.T) {
	_, err := New(Config{IssuerURL: testIssuer, ResourceURL: testResource, StorageURI: "file:" + filepath.Join(t.TempDir(), "o.db")}, nil)
	require.Error(t, err)
}
