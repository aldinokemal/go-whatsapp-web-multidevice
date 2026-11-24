package mcpauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const claudeRedirect = "https://claude.ai/api/mcp/auth_callback"

func TestSubjectIsUserIDNotPhone(t *testing.T) {
	h := newHarness(t, oauth.Config{})
	alice, ck := h.user("+27825550101")
	clientID := h.register(claudeRedirect)
	access, _ := h.tokensFor(ck, clientID, claudeRedirect)
	p, err := h.srv.ValidateAccessToken(context.Background(), access)
	require.NoError(t, err)
	assert.Equal(t, alice.ID, p.Subject)
	assert.NotContains(t, p.Subject, "27825550101")
}

func TestDisabledUserIsNotActive(t *testing.T) {
	h := newHarness(t, oauth.Config{})
	alice, ck := h.user("+27825550101")
	ctx := context.Background()
	assert.True(t, h.authn.Active(ctx, alice.ID))
	clientID := h.register(claudeRedirect)
	access, _ := h.tokensFor(ck, clientID, claudeRedirect)
	require.NoError(t, h.st.SetUserStatus(ctx, alice.ID, store.StatusDisabled))
	assert.False(t, h.authn.Active(ctx, alice.ID))
	assert.False(t, h.authn.Active(ctx, "u_nobody"))
	assert.False(t, h.authn.Active(ctx, ""))
	_, err := h.srv.ValidateAccessToken(ctx, access)
	assert.ErrorIs(t, err, oauth.ErrInvalidToken, "a disabled user's token stops on the next request")
}

func TestConsentCSPWidensFormActionToTheValidatedOriginOnly(t *testing.T) {
	h := newHarness(t, oauth.Config{})
	_, ck := h.user("+27825550101")
	clientID := h.register(claudeRedirect)
	q, _ := authorizeQuery(clientID, claudeRedirect)
	q.Set("state", "x' https://evil.example; script-src *")
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(ck)
	resp, page := h.do(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode, page)
	policy := resp.Header.Get("Content-Security-Policy")
	assert.Contains(t, policy, "form-action 'self' https://claude.ai")
	assert.NotContains(t, policy, "evil.example")
	assert.Equal(t, 1, strings.Count(policy, "form-action"))
	assert.Contains(t, policy, "script-src 'self'")
	assert.Contains(t, page, `data-no-disable`)
	assert.Contains(t, page, `name="decision" value="approve"`)
	assert.Contains(t, page, `name="decision" value="deny"`)
}

func TestConsentPageNeedsTheSessionNotJustAnyone(t *testing.T) {
	h := newHarness(t, oauth.Config{})
	clientID := h.register(claudeRedirect)
	q, _ := authorizeQuery(clientID, claudeRedirect)
	resp, _ := h.do(httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil))
	require.Equal(t, fiber.StatusSeeOther, resp.StatusCode)
	assert.True(t, strings.HasPrefix(resp.Header.Get("Location"), "/signin?next=%2Foauth%2Fauthorize%3F"), resp.Header.Get("Location"))
}

func TestConsentOriginIsTheOriginNotTheURL(t *testing.T) {
	// The redirect URI has a path and query; only its origin reaches the CSP.
	h := newHarness(t, oauth.Config{})
	_, ck := h.user("+27825550101")
	clientID := h.register("https://claude.ai/cb?x=1")
	q, _ := authorizeQuery(clientID, "https://claude.ai/cb?x=1")
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(ck)
	resp, _ := h.do(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.True(t, strings.HasSuffix(resp.Header.Get("Content-Security-Policy"), "form-action 'self' https://claude.ai"))
}

func TestConsentNamesTheClientHostNotTheName(t *testing.T) {
	const id = "https://example.com/oauth/meta"
	client := cimdServer(t, map[string]any{id: cimdDoc(id, "Anthropic Official", claudeRedirect)})
	h := newHarness(t, oauth.Config{DisableDCR: true, CIMD: &oauth.CIMDConfig{AllowedHosts: []string{"example.com"}, HTTPClient: client}})
	_, ck := h.user("+27825550101")
	q, _ := authorizeQuery(id, claudeRedirect)
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(ck)
	resp, page := h.do(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode, page)
	assert.Contains(t, page, `<span class="consent-host">example.com</span> wants to use your WTF account.`)
	assert.NotContains(t, page, `<span class="consent-host">Anthropic Official`)
	assert.Contains(t, page, "Anthropic Official", "the self-asserted name is shown small, below the host")
	assert.NotContains(t, page, "Only continue if you started this from Claude Code", "a web client gets no loopback warning")
}

func TestConsentWarnsWhenTheClientRunsOnThisComputer(t *testing.T) {
	const id = "https://example.com/oauth/code"
	client := cimdServer(t, map[string]any{id: cimdDoc(id, "Claude Code", "http://localhost/callback", "http://127.0.0.1/callback")})
	h := newHarness(t, oauth.Config{DisableDCR: true, CIMD: &oauth.CIMDConfig{AllowedHosts: []string{"example.com"}, HTTPClient: client}})
	_, ck := h.user("+27825550101")
	q, _ := authorizeQuery(id, "http://localhost:43123/callback")
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(ck)
	resp, page := h.do(req)
	require.Equal(t, fiber.StatusOK, resp.StatusCode, page)
	assert.Contains(t, page, "Only continue if you started this from Claude Code")
	assert.True(t, strings.HasSuffix(resp.Header.Get("Content-Security-Policy"), "form-action 'self' http://localhost:43123"))
}

func TestARefusedConnectRequestIsAPage(t *testing.T) {
	h := newHarness(t, oauth.Config{DisableDCR: true, CIMD: &oauth.CIMDConfig{AllowedHosts: []string{"claude.ai"}, HTTPClient: http.DefaultClient}})
	_, ck := h.user("+27825550101")
	q, _ := authorizeQuery("https://evil.example/oauth/meta", claudeRedirect)
	req := httptest.NewRequest("GET", "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(ck)
	resp, page := h.do(req)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	assert.Contains(t, page, "This request to connect an app isn&#39;t valid.")
	assert.Empty(t, resp.Header.Get("Location"), "never redirected to an untrusted callback")
	assert.True(t, strings.HasSuffix(resp.Header.Get("Content-Security-Policy"), "form-action 'self'"), "the refusal renders under the default policy")
}
