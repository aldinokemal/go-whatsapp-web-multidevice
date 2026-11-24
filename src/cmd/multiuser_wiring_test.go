package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultiUserMountsMCPWithOAuthOnly(t *testing.T) {
	app, _, _ := newMultiUserApp(t, uimcp.Deps{})
	for _, auth := range []string{"", "Basic dXNlcjpzZWNyZXQ=", "Bearer nope"} {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, auth)
		challenge := strings.Join(resp.Header.Values("WWW-Authenticate"), ", ")
		assert.Contains(t, challenge, `Bearer resource_metadata="https://wtf.test/.well-known/oauth-protected-resource/mcp"`)
		assert.NotContains(t, challenge, "Basic", "no Basic fallback in multi-user mode")
	}
}

func TestMultiUserServesDiscoveryDocuments(t *testing.T) {
	app, _, _ := newMultiUserApp(t, uimcp.Deps{})
	get := func(path, host string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = host
		req.Header.Set("X-Forwarded-Host", host)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		var out map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return out
	}
	for _, host := range []string{"wtf.test", "evil.example"} {
		pr := get("/.well-known/oauth-protected-resource/mcp", host)
		assert.Equal(t, "https://wtf.test/mcp", pr["resource"], "derived from WTF_PUBLIC_URL, not the Host header")
		assert.Equal(t, []any{"https://wtf.test"}, pr["authorization_servers"])
		as := get("/.well-known/oauth-authorization-server", host)
		assert.Equal(t, "https://wtf.test", as["issuer"])
		assert.Equal(t, "https://wtf.test/oauth/authorize", as["authorization_endpoint"])
		assert.Equal(t, "https://wtf.test/oauth/token", as["token_endpoint"])
		assert.Equal(t, true, as["client_id_metadata_document_supported"])
		_, hasDCR := as["registration_endpoint"]
		assert.False(t, hasDCR)
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/register", strings.NewReader(`{"redirect_uris":["https://claude.ai/cb"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "no registration endpoint: the path falls through to the API gate")
}

func TestOAuthEndpointsSitBeforeTheAPIGate(t *testing.T) {
	app, _, _ := newMultiUserApp(t, uimcp.Deps{})
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader("grant_type=authorization_code&client_id=https%3A%2F%2Fclaude.ai%2Foauth%2Fmcp-oauth-client-metadata&resource=https%3A%2F%2Fwtf.test%2Fmcp"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := app.Test(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, string(body))
	assert.Contains(t, string(body), `"error":"invalid_grant"`, "an OAuth error from the token endpoint, not the API gate's 401")
}

func TestFilesMountedBehindTheAPIGate(t *testing.T) {
	app, _, _ := newMultiUserApp(t, uimcp.Deps{})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/files/d_alice/x.jpg", nil))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

func TestMediaRootHookInstalledAndCleared(t *testing.T) {
	_, rt, _ := newMultiUserApp(t, uimcp.Deps{})
	require.NotNil(t, usecase.MediaRootHook)
	require.NoError(t, rt.Close())
	assert.Nil(t, usecase.MediaRootHook)
}
