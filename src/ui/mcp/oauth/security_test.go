package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		"redirect_uri":          {testRedirect + ".evil.example"},
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
	assert.Equal(t, fiber.StatusBadRequest, register(`{"client_name":"Remote","redirect_uris":["http://example.com/callback"],"application_type":"native","token_endpoint_auth_method":"none"}`))
}
