package web

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGrants is the OAuth server's grant store as the Account page sees it:
// grants per subject; revoking one that is not the subject's fails.
type fakeGrants struct {
	mu      sync.Mutex
	grants  map[string][]oauth.Grant // subject -> grants
	revoked []string
}

func (f *fakeGrants) ListGrants(_ context.Context, subject string) ([]oauth.Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]oauth.Grant(nil), f.grants[subject]...), nil
}

func (f *fakeGrants) RevokeGrant(_ context.Context, subject, family string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, g := range f.grants[subject] {
		if g.FamilyID == family {
			f.grants[subject] = append(f.grants[subject][:i], f.grants[subject][i+1:]...)
			f.revoked = append(f.revoked, family)
			return nil
		}
	}
	return oauth.ErrInvalidGrant
}

func newEnvWithGrants(t *testing.T, g GrantStore, mcpURL string) *env {
	t.Helper()
	e := newEnv(t)
	e.d.Grants, e.d.MCPURL = g, mcpURL
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	app.Use(e.d.Auth.Middleware())
	Mount(app, e.d)
	app.Use(e.d.Auth.APIKey(), e.d.Auth.RequireAPI())
	e.app = app
	return e
}

var addLinkRE = regexp.MustCompile(`href="(https://claude\.ai/customize/connectors\?[^"]+)"`)

func TestConnectLinkIsURLEncoded(t *testing.T) {
	const tricky = "https://wtf.example.com/mcp?a=b&c=d"
	e := newEnvWithGrants(t, &fakeGrants{}, tricky)
	ck, _ := signedIn(t, e, "+27825550101")
	body := get(t, e, ck, "/account")
	m := addLinkRE.FindStringSubmatch(body)
	require.NotNil(t, m, "the Connect Claude dialog has an Add to Claude link")
	u, err := url.Parse(html.UnescapeString(m[1]))
	require.NoError(t, err)
	assert.Equal(t, "add-custom-connector", u.Query().Get("modal"))
	assert.Equal(t, "WTF", u.Query().Get("connectorName"))
	assert.Equal(t, tricky, u.Query().Get("connectorUrl"), "the MCP URL round-trips exactly")
	assert.Len(t, u.Query(), 3, "nothing from the MCP URL leaked into claude.ai's own query")
	assert.Contains(t, body, `value="`+html.EscapeString(tricky)+`"`, "the address is shown for manual entry")
	assert.Contains(t, body, "claude mcp add --transport http wtf ")
}

func TestGrantsListOnlyMine(t *testing.T) {
	g := &fakeGrants{grants: map[string][]oauth.Grant{}}
	e := newEnvWithGrants(t, g, "https://wtf.example.com/mcp")
	ck, aliceID := signedIn(t, e, "+27825550101")
	_, bobID := signedIn(t, e, "+27825550102")
	now := time.Now()
	g.grants[aliceID] = []oauth.Grant{{FamilyID: "fam_alice", ClientHost: "claude.ai", CreatedAt: now.Add(-time.Hour), LastUsedAt: now}}
	g.grants[bobID] = []oauth.Grant{{FamilyID: "fam_bob", ClientHost: "bob-only.example", CreatedAt: now, LastUsedAt: now}}
	body := get(t, e, ck, "/account")
	assert.Contains(t, body, "fam_alice")
	assert.Contains(t, body, `<p class="row-title">claude.ai</p>`, "with no trusted name, the host is the title")
	assert.NotContains(t, body, "fam_bob")
	assert.NotContains(t, body, "bob-only.example")
	assert.Contains(t, body, "1 app<")
	assert.Contains(t, body, `data-open-dialog="disconnect-app"`, "Disconnect asks first")
}

func TestDisconnectRevokesMyGrantAndRefusesAnotherUsers(t *testing.T) {
	g := &fakeGrants{grants: map[string][]oauth.Grant{}}
	e := newEnvWithGrants(t, g, "https://wtf.example.com/mcp")
	ck, aliceID := signedIn(t, e, "+27825550101")
	_, bobID := signedIn(t, e, "+27825550102")
	g.grants[aliceID] = []oauth.Grant{{FamilyID: "fam_alice", ClientHost: "claude.ai"}}
	g.grants[bobID] = []oauth.Grant{{FamilyID: "fam_bob", ClientHost: "claude.ai"}}
	csrf := csrfFrom(t, get(t, e, ck, "/account"))
	post := func(path string) *http.Response {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"csrf": {csrf}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(ck)
		resp, _ := e.do(t, req)
		return resp
	}
	resp := post("/account/connections/fam_bob/revoke")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp = post("/account/connections/fam_nope/revoke")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp = post("/account/connections/fam_alice/revoke")
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, []string{"fam_alice"}, g.revoked)
	assert.Len(t, g.grants[bobID], 1, "Bob's connection is untouched")
}

func TestDisconnectNeedsCSRFAndASession(t *testing.T) {
	g := &fakeGrants{grants: map[string][]oauth.Grant{}}
	e := newEnvWithGrants(t, g, "https://wtf.example.com/mcp")
	ck, aliceID := signedIn(t, e, "+27825550101")
	g.grants[aliceID] = []oauth.Grant{{FamilyID: "fam_alice", ClientHost: "claude.ai"}}
	req := httptest.NewRequest(http.MethodPost, "/account/connections/fam_alice/revoke", nil)
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	raw, _, err := e.st.CreateAPIKey(context.Background(), aliceID, "k")
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodPost, "/account/connections/fam_alice/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, _ = e.do(t, req)
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.True(t, strings.HasPrefix(resp.Header.Get("Location"), "/signin"), "an API key never reaches a page route")
	assert.Empty(t, g.revoked)
}

func TestNoConnectedAppsCardWhenMCPIsOff(t *testing.T) {
	e := newEnv(t)
	ck, _ := signedIn(t, e, "+27825550101")
	body := get(t, e, ck, "/account")
	assert.NotContains(t, body, "Connected apps")
	assert.NotContains(t, body, "Connect Claude")
}

func TestConnectorSlug(t *testing.T) {
	assert.Equal(t, "wtf", connectorSlug("WTF"))
	assert.Equal(t, "acme-whatsapp", connectorSlug("Acme WhatsApp!"))
	assert.Equal(t, "whatsapp", connectorSlug("¡¡"))
}
