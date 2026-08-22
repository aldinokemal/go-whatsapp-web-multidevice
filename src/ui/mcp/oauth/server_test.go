package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testIssuer   = "https://gowa.example.com"
	testResource = "https://gowa.example.com/mcp"
	testRedirect = "https://claude.ai/api/mcp/auth_callback"
)

func newOAuthTestServer(t *testing.T, issuer, resource string) (*Server, *fiber.App) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "oauth.db")
	srv, err := New(Config{
		IssuerURL:   issuer,
		ResourceURL: resource,
		StorageURI:  "file:" + filepath.ToSlash(dbPath),
	}, func(username, password string) bool {
		return username == "user" && password == "secret"
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, srv.Close()) })

	app := fiber.New()
	srv.RegisterPublic(app)
	return srv, app
}

func TestMetadataUsesRFCWellKnownPathInsertion(t *testing.T) {
	_, app := newOAuthTestServer(t,
		"https://gowa.example.com/gowa",
		"https://gowa.example.com/gowa/mcp",
	)

	resp, err := app.Test(httptest.NewRequest("GET", "/.well-known/oauth-authorization-server/gowa", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var authMetadata map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&authMetadata))
	assert.Equal(t, "https://gowa.example.com/gowa", authMetadata["issuer"])
	assert.Equal(t, "https://gowa.example.com/gowa/oauth/authorize", authMetadata["authorization_endpoint"])
	assert.Equal(t, "https://gowa.example.com/gowa/oauth/token", authMetadata["token_endpoint"])
	assert.Equal(t, "https://gowa.example.com/gowa/oauth/register", authMetadata["registration_endpoint"])
	assert.Equal(t, []any{"S256"}, authMetadata["code_challenge_methods_supported"])
	assert.Equal(t, true, authMetadata["authorization_response_iss_parameter_supported"])
	_, claimsCIMD := authMetadata["client_id_metadata_document_supported"]
	assert.False(t, claimsCIMD, "DCR compatibility must not claim unimplemented CIMD support")

	resp, err = app.Test(httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/gowa/mcp", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)

	var resourceMetadata map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&resourceMetadata))
	assert.Equal(t, "https://gowa.example.com/gowa/mcp", resourceMetadata["resource"])
	assert.Equal(t, []any{"https://gowa.example.com/gowa"}, resourceMetadata["authorization_servers"])
}

func TestDynamicClientRegistrationRequiresSafeExactRedirects(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)

	register := func(body string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		var payload map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		return resp.StatusCode, payload
	}

	status, payload := register(`{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"application_type":"web","token_endpoint_auth_method":"none"}`)
	assert.Equal(t, fiber.StatusCreated, status)
	assert.NotEmpty(t, payload["client_id"])
	assert.Equal(t, "none", payload["token_endpoint_auth_method"])

	status, payload = register(`{"client_name":"Bad","redirect_uris":["http://evil.example/callback"],"application_type":"web","token_endpoint_auth_method":"none"}`)
	assert.Equal(t, fiber.StatusBadRequest, status)
	assert.Equal(t, "invalid_redirect_uri", payload["error"])
}

func TestDynamicClientRegistrationReturnsActualFixedCapabilities(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	body := `{"client_name":"Fixed capabilities","redirect_uris":["https://example.com/callback"],"grant_types":["authorization_code"],"response_types":["code"]}`
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)

	var registration map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&registration))
	assert.Equal(t, []any{"authorization_code", "refresh_token"}, registration["grant_types"])
	assert.Equal(t, []any{"code"}, registration["response_types"])
}

func TestAuthorizationCodeFlowBearerAndBasicFallback(t *testing.T) {
	srv, app := newOAuthTestServer(t, testIssuer, testResource)
	clientID := registerTestClient(t, app)

	verifier := strings.Repeat("a", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	authorizeQuery := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {testRedirect},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {testResource},
		"scope":                 {"mcp"},
		"state":                 {"state-123"},
	}

	resp, err := app.Test(httptest.NewRequest("GET", "/oauth/authorize?"+authorizeQuery.Encode(), nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	loginBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(loginBody), "Authorize Claude")
	assert.Contains(t, string(loginBody), "After authorization, you'll continue to <strong>claude.ai</strong>.")
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'")
	assert.Contains(t, resp.Header.Get("Content-Security-Policy"), "form-action 'self' https://claude.ai;")

	loginForm := authorizeQuery
	loginForm.Set("username", "user")
	loginForm.Set("password", "secret")
	loginReq := httptest.NewRequest("POST", "/oauth/authorize", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = app.Test(loginReq)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusFound, resp.StatusCode)

	callback, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	code := callback.Query().Get("code")
	require.NotEmpty(t, code)
	assert.Equal(t, "state-123", callback.Query().Get("state"))
	assert.Equal(t, testIssuer, callback.Query().Get("iss"))

	tokens := exchangeTestCode(t, app, clientID, code, verifier, testResource)
	require.NotEmpty(t, tokens.AccessToken)
	require.NotEmpty(t, tokens.RefreshToken)
	assert.Equal(t, "Bearer", tokens.TokenType)
	assert.Positive(t, tokens.ExpiresIn)

	protected := app.Group("", srv.MCPAuthMiddleware(func(username, password string) bool {
		return username == "user" && password == "secret"
	}))
	protected.Post("/mcp", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	bearerReq := httptest.NewRequest("POST", "/mcp", nil)
	bearerReq.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	resp, err = app.Test(bearerReq)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

	basicReq := httptest.NewRequest("POST", "/mcp", nil)
	basicReq.SetBasicAuth("user", "secret")
	resp, err = app.Test(basicReq)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)

	resp, err = app.Test(httptest.NewRequest("POST", "/mcp", nil))
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("WWW-Authenticate"), `Bearer resource_metadata="https://gowa.example.com/.well-known/oauth-protected-resource/mcp"`)
}

func TestTokenExchangeRejectsWrongResourceAndCodeReplay(t *testing.T) {
	_, app := newOAuthTestServer(t, testIssuer, testResource)
	clientID := registerTestClient(t, app)
	code, verifier := authorizeTestClient(t, app, clientID)

	bad := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {testRedirect},
		"code_verifier": {verifier},
		"resource":      {"https://gowa.example.com/other"},
	}
	resp := postForm(t, app, "/oauth/token", bad)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_target")

	_ = exchangeTestCode(t, app, clientID, code, verifier, testResource)

	replay := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {testRedirect},
		"code_verifier": {verifier},
		"resource":      {testResource},
	}
	resp = postForm(t, app, "/oauth/token", replay)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_grant")
}

func TestRefreshRotationDetectsReuseAndRevokesFamily(t *testing.T) {
	srv, app := newOAuthTestServer(t, testIssuer, testResource)
	clientID := registerTestClient(t, app)
	code, verifier := authorizeTestClient(t, app, clientID)
	initial := exchangeTestCode(t, app, clientID, code, verifier, testResource)

	firstRefresh := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {initial.RefreshToken},
		"resource":      {testResource},
	}
	resp := postForm(t, app, "/oauth/token", firstRefresh)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	var rotated tokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rotated))
	require.NotEmpty(t, rotated.AccessToken)
	require.NotEmpty(t, rotated.RefreshToken)
	assert.NotEqual(t, initial.RefreshToken, rotated.RefreshToken)

	resp = postForm(t, app, "/oauth/token", firstRefresh)
	require.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assertOAuthError(t, resp, "invalid_grant")

	protected := app.Group("", srv.MCPAuthMiddleware(nil))
	protected.Post("/mcp", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })
	bearerReq := httptest.NewRequest("POST", "/mcp", nil)
	bearerReq.Header.Set("Authorization", "Bearer "+rotated.AccessToken)
	resp, err := app.Test(bearerReq)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode, "reuse must revoke access tokens in the refresh family")
}

func registerTestClient(t *testing.T, app *fiber.App) string {
	t.Helper()
	body := `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"application_type":"web","token_endpoint_auth_method":"none"}`
	req := httptest.NewRequest("POST", "/oauth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusCreated, resp.StatusCode)
	var payload struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	require.NotEmpty(t, payload.ClientID)
	return payload.ClientID
}

func authorizeTestClient(t *testing.T, app *fiber.App, clientID string) (string, string) {
	t.Helper()
	verifier := strings.Repeat("b", 43)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	form := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {testRedirect},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"resource":              {testResource},
		"scope":                 {"mcp"},
		"state":                 {"state-123"},
		"username":              {"user"},
		"password":              {"secret"},
	}
	resp := postForm(t, app, "/oauth/authorize", form)
	require.Equal(t, fiber.StatusFound, resp.StatusCode)
	callback, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	code := callback.Query().Get("code")
	require.NotEmpty(t, code)
	return code, verifier
}

func exchangeTestCode(t *testing.T, app *fiber.App, clientID, code, verifier, resource string) tokenResponse {
	t.Helper()
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {testRedirect},
		"code_verifier": {verifier},
		"resource":      {resource},
	}
	resp := postForm(t, app, "/oauth/token", form)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	var payload tokenResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	return payload
}

func postForm(t *testing.T, app *fiber.App, path string, form url.Values) *http.Response {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func assertOAuthError(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
	assert.Equal(t, want, payload["error"])
}
