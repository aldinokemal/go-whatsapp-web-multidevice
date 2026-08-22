package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func TestAuthorizationRejectsRedirectPrefixMatch(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	clientID := registerTestClient(t, app)
	verifier := strings.Repeat("c", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {"https://claude.ai.evil.example/api/mcp/auth_callback"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {testResource},
		"scope":                 {"mcp"},
	}
	resp, err := app.Test(httptest.NewRequest("GET", "/oauth/authorize?"+query.Encode(), nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_request")
}

func TestAuthorizationPOSTStopsAfterRedirectValidationError(t *testing.T) {
	srv, app := newOAuthTestServer(t, testIssuer, testResource)
	clientID := registerTestClient(t, app)
	verifier := strings.Repeat("c", 43)
	sum := sha256.Sum256([]byte(verifier))

	form := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {"https://claude.ai.evil.example/api/mcp/auth_callback"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {testResource},
		"scope":                 {"mcp"},
		"username":              {"user"},
		"password":              {"secret"},
	}
	resp := postForm(t, app, "/oauth/authorize", form)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_request")

	var codes int
	require.NoError(t, srv.store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM oauth_authorization_codes`).Scan(&codes))
	assert.Zero(t, codes, "a rejected authorization request must never issue a code")
}

func TestAuthorizationReturnsSafeValidationErrorsToClient(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	clientID := registerTestClient(t, app)
	verifier := strings.Repeat("f", 43)
	sum := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {testRedirect},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {testResource},
		"scope":                 {"admin"},
		"state":                 {"state-123"},
	}
	resp, err := app.Test(httptest.NewRequest("GET", "/oauth/authorize?"+query.Encode(), nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusFound, resp.StatusCode)
	callback, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, testRedirect, callback.Scheme+"://"+callback.Host+callback.Path)
	assert.Equal(t, "invalid_scope", callback.Query().Get("error"))
	assert.Equal(t, "state-123", callback.Query().Get("state"))
	assert.Equal(t, testIssuer, callback.Query().Get("iss"))
	assert.Empty(t, callback.Query().Get("code"))
}

func TestWrongPKCEVerifierDoesNotConsumeAuthorizationCode(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	clientID := registerTestClient(t, app)
	code, verifier := authorizeTestClient(t, app, clientID)

	wrongVerifier := strings.Repeat("d", 43)
	wrong := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {testRedirect},
		"code_verifier": {wrongVerifier},
		"resource":      {testResource},
	}
	resp := postForm(t, app, "/oauth/token", wrong)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_grant")

	// A failed verifier must not burn the code; the legitimate client still
	// gets exactly one successful redemption with the original verifier.
	pair := exchangeTestCode(t, app, clientID, code, verifier, testResource)
	assert.NotEmpty(t, pair.AccessToken)
}

func TestDCRNativeRedirectAllowsOnlyLoopbackHTTP(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)

	register := func(body string) int {
		t.Helper()
		req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp.StatusCode
	}

	assert.Equal(t, fiber.StatusCreated, register(`{"client_name":"Local","redirect_uris":["http://127.0.0.1:49152/callback"],"application_type":"native","token_endpoint_auth_method":"none"}`))
	assert.Equal(t, fiber.StatusCreated, register(`{"client_name":"Local v6","redirect_uris":["http://[::1]:49152/callback"],"application_type":"native","token_endpoint_auth_method":"none"}`))
	assert.Equal(t, fiber.StatusCreated, register(`{"client_name":"Named","redirect_uris":["http://localhost:49152/callback"],"application_type":"native","token_endpoint_auth_method":"none"}`))
	assert.Equal(t, fiber.StatusCreated, register(`{"client_name":"Named upper","redirect_uris":["http://LocalHost:49152/callback"],"application_type":"native","token_endpoint_auth_method":"none"}`))
	assert.Equal(t, fiber.StatusBadRequest, register(`{"client_name":"Remote","redirect_uris":["http://example.com/callback"],"application_type":"native","token_endpoint_auth_method":"none"}`))
	assert.Equal(t, fiber.StatusBadRequest, register(`{"client_name":"Lookalike","redirect_uris":["http://localhost.example.com:49152/callback"],"application_type":"native","token_endpoint_auth_method":"none"}`))
	assert.Equal(t, fiber.StatusBadRequest, register(`{"client_name":"No port","redirect_uris":["http://localhost/callback"],"application_type":"native","token_endpoint_auth_method":"none"}`))
	assert.Equal(t, fiber.StatusBadRequest, register(`{"client_name":"Web","redirect_uris":["http://localhost:49152/callback"],"application_type":"web","token_endpoint_auth_method":"none"}`))
}

func TestDCRAcceptsClaudeCodeNativeLocalhostRegistration(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	body := `{"client_name":"Claude Code (gowa)","redirect_uris":["http://localhost:60390/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none","application_type":"native","scope":"mcp"}`
	req := httptest.NewRequestWithContext(t.Context(), "POST", "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var registered struct {
		ClientID        string   `json:"client_id"`
		RedirectURIs    []string `json:"redirect_uris"`
		ApplicationType string   `json:"application_type"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&registered))
	assert.NotEmpty(t, registered.ClientID)
	assert.Equal(t, []string{"http://localhost:60390/callback"}, registered.RedirectURIs)
	assert.Equal(t, "native", registered.ApplicationType)

	verifier := strings.Repeat("c", 43)
	sum := sha256.Sum256([]byte(verifier))
	authorizeQuery := url.Values{
		"response_type":         {"code"},
		"client_id":             {registered.ClientID},
		"redirect_uri":          {"http://localhost:60390/callback"},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {testResource},
		"scope":                 {"mcp"},
		"state":                 {"state-123"},
	}
	resp, err = app.Test(httptest.NewRequestWithContext(t.Context(), "GET", "/oauth/authorize?"+authorizeQuery.Encode(), nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	loginBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(loginBody), "After authorization, you'll return to <strong>Claude Code (gowa)</strong> on this device (<strong>localhost:60390</strong>).")
	assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "form-action 'self' http://localhost:60390;")
}

func TestNativeLocalhostRedirectStillRequiresExactMatch(t *testing.T) {
	client := Client{
		ApplicationType: "native",
		RedirectURIs:    []string{"http://localhost:60390/callback"},
	}

	assert.True(t, redirectURIMatches(client, "http://localhost:60390/callback"))
	assert.False(t, redirectURIMatches(client, "http://localhost:60391/callback"))
	assert.False(t, redirectURIMatches(client, "http://localhost:60390/other"))
	assert.False(t, redirectURIMatches(client, "http://127.0.0.1:60390/callback"))
}

func TestNativeLoopbackRedirectAllowsEphemeralPort(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	registeredRedirect := "http://127.0.0.1:49152/callback"
	requestedRedirect := "http://127.0.0.1:53100/callback"
	registrationBody := `{"client_name":"Local","redirect_uris":["` + registeredRedirect + `"],"application_type":"native","token_endpoint_auth_method":"none"}`
	registration := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(registrationBody))
	registration.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(registration)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)
	var registered struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&registered))

	verifier := strings.Repeat("e", 43)
	sum := sha256.Sum256([]byte(verifier))
	form := url.Values{
		"response_type":         {"code"},
		"client_id":             {registered.ClientID},
		"redirect_uri":          {requestedRedirect},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {testResource},
		"scope":                 {"mcp"},
		"username":              {"user"},
		"password":              {"secret"},
	}
	form.Set("redirect_uri", "http://127.0.0.1:53100/other")
	resp = postForm(t, app, "/oauth/authorize", form)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_request")

	form.Set("redirect_uri", requestedRedirect)
	resp = postForm(t, app, "/oauth/authorize", form)
	require.Equal(t, fiber.StatusFound, resp.StatusCode)
	callback, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	code := callback.Query().Get("code")
	require.NotEmpty(t, code)
	assert.Equal(t, requestedRedirect, callback.Scheme+"://"+callback.Host+callback.Path)

	wrongRedirect := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {registered.ClientID},
		"code":          {code},
		"redirect_uri":  {registeredRedirect},
		"code_verifier": {verifier},
		"resource":      {testResource},
	}
	resp = postForm(t, app, "/oauth/token", wrongRedirect)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_grant")

	wrongRedirect.Set("redirect_uri", requestedRedirect)
	resp = postForm(t, app, "/oauth/token", wrongRedirect)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
}

func TestDCRRejectsOversizedMetadataBeforeStorage(t *testing.T) {
	srv, app := newOAuthTestServer(t, testIssuer, testResource)
	body := `{"client_name":"Large","redirect_uris":["https://example.com/callback"],"padding":"` + strings.Repeat("x", maxOAuthBodyBytes) + `"}`
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.ErrorIs(t, err, fasthttp.ErrBodyTooLarge)
	assert.Nil(t, resp)

	var clients int
	require.NoError(t, srv.store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM oauth_clients`).Scan(&clients))
	assert.Zero(t, clients)
}

func TestDCRRejectsOversizedRedirectURI(t *testing.T) {
	srv, app := newOAuthTestServer(t, testIssuer, testResource)
	redirect := "https://example.com/" + strings.Repeat("x", maxRedirectURIBytes)
	body := `{"client_name":"Large redirect","redirect_uris":["` + redirect + `"],"application_type":"web","token_endpoint_auth_method":"none"}`
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_redirect_uri")

	var clients int
	require.NoError(t, srv.store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM oauth_clients`).Scan(&clients))
	assert.Zero(t, clients)
}

func TestDCRAppliesDurableClientQuota(t *testing.T) {
	srv, app := newOAuthTestServer(t, testIssuer, testResource)
	srv.store.maxClients = 1
	srv.store.maxRegistrationsPerWindow = 10
	_ = registerTestClient(t, app)

	body := `{"client_name":"Second","redirect_uris":["https://example.com/callback"],"application_type":"web","token_endpoint_auth_method":"none"}`
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusTooManyRequests, resp.StatusCode)
	assertOAuthError(t, resp, "temporarily_unavailable")

	var clients int
	require.NoError(t, srv.store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM oauth_clients`).Scan(&clients))
	assert.Equal(t, 1, clients)
}

func TestOAuthPOSTEndpointsRejectOversizedBodiesAtServerBoundary(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	for _, endpoint := range []struct {
		name        string
		path        string
		contentType string
	}{
		{name: "register", path: "/oauth/register", contentType: "application/json"},
		{name: "authorize", path: "/oauth/authorize", contentType: "application/x-www-form-urlencoded"},
		{name: "token", path: "/oauth/token", contentType: "application/x-www-form-urlencoded"},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", endpoint.path, strings.NewReader(strings.Repeat("x", maxOAuthBodyBytes+1)))
			req.Header.Set("Content-Type", endpoint.contentType)
			resp, err := app.Test(req)
			require.ErrorIs(t, err, fasthttp.ErrBodyTooLarge)
			assert.Nil(t, resp)
		})
	}
}

// Fiber runs with CaseSensitive and StrictRouting disabled, so trailing-slash,
// case, and absolute-form targets all reach the same OAuth handlers. The
// header-time cap must cover each of them instead of the byte-exact path only.
func TestOAuthBodyCapCoversRoutingPathAliases(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	for _, target := range []string{
		"/oauth/token/",
		"/oauth/token//",
		"/OAUTH/TOKEN",
		"/oauth/Token/",
		"/oauth/token?resource=https://gowa.example.com/mcp",
		"http://gowa.example.com/oauth/token/",
		"/oauth/authorize/",
		"/OAUTH/AUTHORIZE",
		"/oauth/register/",
		"/OAUTH/REGISTER",
	} {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest("POST", target, strings.NewReader(strings.Repeat("x", maxOAuthBodyBytes+1)))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := app.Test(req)
			require.ErrorIs(t, err, fasthttp.ErrBodyTooLarge)
			assert.Nil(t, resp)
		})
	}
}

// The cap must stay scoped to the OAuth POST routes so the application's own
// large-upload endpoints keep the app-wide BodyLimit.
func TestOAuthBodyCapLeavesOtherRoutesAlone(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	app.Post("/send/video", func(c fiber.Ctx) error { return c.SendString("ok") })

	req := httptest.NewRequest("POST", "/send/video", strings.NewReader(strings.Repeat("x", maxOAuthBodyBytes+1)))
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
}
