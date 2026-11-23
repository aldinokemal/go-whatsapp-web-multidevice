package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/mcpauth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Phase 2 isolation: the new ways in (API keys, /files, /mcp with an OAuth
// token or a key, OAuth grants) never reach another person's numbers, and a
// disabled person is refused on every one of them on the very next request.
// The app is the real multi-user wiring (Setup + Install); the only test
// seam is the CIMD client, which reaches a local document server for
// "example.com" (netguard rightly refuses loopback).

const testCIMDClientID = "https://example.com/oauth/mcp-oauth-client-metadata"
const testCallback = "https://claude.ai/api/mcp/auth_callback"

func cimdTestClient(t *testing.T) *http.Client {
	t.Helper()
	doc := map[string]any{"client_id": testCIMDClientID, "client_name": "Claude", "redirect_uris": []string{testCallback},
		"token_endpoint_auth_method": "none", "grant_types": []string{"authorization_code", "refresh_token"}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/mcp-oauth-client-metadata" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)
	tr := srv.Client().Transport.(*http.Transport).Clone()
	addr := srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: tr, Timeout: 5 * time.Second}
}

type p2 struct {
	t        *testing.T
	app      *fiber.App
	rt       *multiuser.Runtime
	alice    *store.User
	aliceCk  *http.Cookie
	csrf     string
	aliceKey string
	aliceAT  string
	bobAT    string
	bobKey   string
	bob      *store.User
	adminKey string
}

// p2AdminPhone is the superadmin of the phase 2 environment.
const p2AdminPhone = "+27825550100"

func newP2(t *testing.T) *p2 {
	t.Helper()
	client := cimdTestClient(t)
	app, rt, _ := newMultiUserAppWith(t, uimcp.Deps{}, func(s *multiuser.Settings) {
		s.MCPClientHosts = []string{"example.com"}
		s.CIMDHTTPClient = client
		s.Superadmins = map[string]bool{p2AdminPhone: true}
	})
	ctx := context.Background()
	alice, err := rt.Store.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	bob, err := rt.Store.CreateUser(ctx, "+27825550102")
	require.NoError(t, err)
	require.NoError(t, rt.Store.SetOwner(ctx, "d_alice", alice.ID))
	require.NoError(t, rt.Store.SetOwner(ctx, "d_bob", bob.ID))
	rawAlice, _ := rt.Store.CreateSession(ctx, alice.ID, "", time.Hour)
	rawBob, _ := rt.Store.CreateSession(ctx, bob.ID, "", time.Hour)
	aliceKey, _, err := rt.Store.CreateAPIKey(ctx, alice.ID, "laptop")
	require.NoError(t, err)
	bobKey, _, err := rt.Store.CreateAPIKey(ctx, bob.ID, "laptop")
	require.NoError(t, err)
	admin, err := rt.Store.CreateUser(ctx, p2AdminPhone)
	require.NoError(t, err)
	adminKey, _, err := rt.Store.CreateAPIKey(ctx, admin.ID, "laptop")
	require.NoError(t, err)

	// Media: one file each, under the real media root.
	for id, name := range map[string]string{"d_alice": "a.jpg", "d_bob": "b.jpg"} {
		dir := filepath.Join(rt.MediaRoot, id)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("\xff\xd8\xff secret of "+id), 0o644))
	}

	e := &p2{t: t, app: app, rt: rt, alice: alice, aliceKey: aliceKey, bobKey: bobKey, bob: bob, adminKey: adminKey,
		aliceCk: &http.Cookie{Name: rt.Auth.CookieName(), Value: rawAlice}, csrf: csrfForRaw(rt, rawAlice)}
	e.aliceAT = e.connect(e.aliceCk)
	e.bobAT = e.connect(&http.Cookie{Name: rt.Auth.CookieName(), Value: rawBob})
	return e
}

func (e *p2) do(req *http.Request) (*http.Response, string) {
	e.t.Helper()
	resp, err := e.app.Test(req)
	require.NoError(e.t, err)
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

// connect runs the real browser flow: authorize (consent page), Allow, then
// the code exchange, and returns the access token.
func (e *p2) connect(ck *http.Cookie) string {
	e.t.Helper()
	verifier := strings.Repeat("z", 43)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{"response_type": {"code"}, "client_id": {testCIMDClientID}, "redirect_uri": {testCallback},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"resource": {muTestPublicURL + "/mcp"}, "scope": {"mcp"}, "state": {"s"}}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(ck)
	resp, page := e.do(req)
	require.Equal(e.t, http.StatusOK, resp.StatusCode, page)
	const marker = `name="csrf" value="`
	i := strings.Index(page, marker)
	require.GreaterOrEqual(e.t, i, 0)
	csrf := page[i+len(marker):]
	csrf = csrf[:strings.IndexByte(csrf, '"')]
	form := url.Values{}
	for k, v := range q {
		form[k] = v
	}
	form.Set("csrf", csrf)
	form.Set("decision", "approve")
	req = httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	resp, body := e.do(req)
	require.Equal(e.t, http.StatusFound, resp.StatusCode, body)
	cb, _ := url.Parse(resp.Header.Get("Location"))
	tok := url.Values{"grant_type": {"authorization_code"}, "client_id": {testCIMDClientID}, "code": {cb.Query().Get("code")},
		"redirect_uri": {testCallback}, "code_verifier": {verifier}, "resource": {muTestPublicURL + "/mcp"}}
	req = httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(tok.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, body = e.do(req)
	require.Equal(e.t, http.StatusOK, resp.StatusCode, body)
	var pair struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(e.t, json.Unmarshal([]byte(body), &pair))
	return pair.AccessToken
}

func (e *p2) mcp(token, deviceHeader, body string) (int, string) {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if deviceHeader != "" {
		req.Header.Set(middleware.DeviceIDHeader, deviceHeader)
	}
	resp, raw := e.do(req)
	return resp.StatusCode, raw
}

func callTool(name string, args map[string]any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	return string(b)
}

// toolArgs: for each device-scoped tool (upstream's and WTF's for the REST
// routes upstream lacked), arguments its schema accepts, so a refusal can
// only come from the device check (proved by the own-device control in
// TestMCPNeverReachesAnotherUsersDevice).
var toolArgs = map[string]map[string]any{
	"whatsapp_send":         {"type": "text", "phone": "27825550199", "message": "hi"},
	"whatsapp_schedule":     {"action": "list"},
	"whatsapp_message":      {"action": "mark_read", "phone": "27825550199", "message_id": "ABC"},
	"whatsapp_chat":         {"action": "list_chats"},
	"whatsapp_group":        {"action": "info", "group_id": "123@g.us"},
	"whatsapp_app":          {"action": "status"},
	uimcp.ToolContactInfo:   {"action": "check", "phone": "27825550199"},
	uimcp.ToolMyWhatsApp:    {"action": "privacy"},
	uimcp.ToolSetPushName:   {"push_name": "Alice"},
	uimcp.ToolChatSettings:  {"action": "pin", "chat_jid": "27825550199@s.whatsapp.net", "pinned": true},
	uimcp.ToolPresence:      {"action": "presence", "type": "available"},
	uimcp.ToolRejectCall:    {"caller_jid": "27825550199@s.whatsapp.net", "call_id": "C1"},
	uimcp.ToolNewsletter:    {"action": "messages", "newsletter_id": "1@newsletter"},
	uimcp.ToolGroupFromLink: {"link": "https://chat.whatsapp.com/AbC"},
}

// numberTools: WTF's tools for one of the caller's own numbers (My
// WhatsApp). Each takes device_id; another person's number must read
// exactly like one that does not exist. Unlink runs last in the controls.
var numberTools = map[string]map[string]any{
	mcpauth.LinkNumberTool:      {},
	mcpauth.NumberStatusTool:    {},
	mcpauth.ReconnectNumberTool: {},
	mcpauth.RenameNumberTool:    {"label": "mine now"},
	mcpauth.GetWebhookTool:      {},
	mcpauth.SetWebhookTool:      {"url": "https://1.1.1.1/hook", "secret": "x"},
	mcpauth.RemoveWebhookTool:   {},
	mcpauth.UnlinkNumberTool:    {},
}

// accountTools act on the caller's own account only; none takes an id.
var accountTools = []string{mcpauth.MyDevicesTool, mcpauth.MyAccountTool, mcpauth.MySessionsTool,
	mcpauth.MyConnectedAppsTool, mcpauth.MyAPIKeysTool, mcpauth.MyPasskeysTool}

func withDevice(args map[string]any, id string) map[string]any {
	out := map[string]any{"device_id": id}
	for k, v := range args {
		out[k] = v
	}
	return out
}

func isDeviceRefusal(status int, raw string) bool {
	// A per-call device_id that is not the caller's reads like an unknown one;
	// an X-Device-Id header that is not the caller's leaves no device at all.
	return status == http.StatusOK && strings.Contains(raw, `"isError":true`) &&
		(strings.Contains(raw, "device not found") || strings.Contains(raw, "device identification required"))
}

func listedTools(t *testing.T, raw string) []string {
	t.Helper()
	var body struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &body), raw)
	var names []string
	for _, tl := range body.Result.Tools {
		names = append(names, tl.Name)
	}
	return names
}

// The pinned list: every tool /mcp serves is in exactly one group of this
// matrix. A new tool fails here until it joins one, and so its isolation
// check.
func TestEveryMCPToolIsInTheIsolationMatrix(t *testing.T) {
	e := newP2(t)
	var want []string
	for name := range toolArgs {
		want = append(want, name)
	}
	for name := range numberTools {
		want = append(want, name)
	}
	want = append(want, accountTools...)
	_, list := e.mcp(e.aliceAT, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.ElementsMatch(t, want, listedTools(t, list), "a member's list: a new tool must join toolArgs, numberTools or accountTools")
	_, list = e.mcp(e.adminKey, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.ElementsMatch(t, append(want, mcpauth.AdminTools...), listedTools(t, list), "the admin's list: a new admin tool must join mcpauth.AdminTools")
}

func TestMCPNeverReachesAnotherUsersDevice(t *testing.T) {
	e := newP2(t)
	for name, args := range toolArgs {
		for cred, tok := range map[string]string{"oauth": e.aliceAT, "api_key": e.aliceKey} {
			// Control: Alice's own device passes validation and the device
			// check (the empty usecase behind it then fails, never as
			// "device not found").
			status, raw := e.mcp(tok, "", callTool(name, withDevice(args, "d_alice")))
			// (500: the tool reached its usecase, which is empty here.)
			assert.Equal(t, http.StatusInternalServerError, status, "%s/%s own device must reach the usecase: %s", name, cred, raw)

			status, raw = e.mcp(tok, "d_bob", callTool(name, args))
			assert.True(t, isDeviceRefusal(status, raw), "%s/%s via X-Device-Id d_bob: %d %s", name, cred, status, raw)
			status, raw = e.mcp(tok, "", callTool(name, withDevice(args, "d_bob")))
			assert.True(t, isDeviceRefusal(status, raw), "%s/%s via device_id d_bob: %d %s", name, cred, status, raw)
			status, raw = e.mcp(tok, "d_alice", callTool(name, withDevice(args, "d_bob")))
			assert.True(t, isDeviceRefusal(status, raw), "%s/%s device_id overrides an owned header: %d %s", name, cred, status, raw)
		}
	}
}

func TestMCPNumberToolsNeverReachAnotherUsersNumber(t *testing.T) {
	drainBroadcasts()
	e := newP2(t)
	ctx := context.Background()
	for name, args := range numberTools {
		for cred, tok := range map[string]string{"oauth": e.aliceAT, "api_key": e.aliceKey} {
			for _, target := range []string{"d_bob", "d_ghost"} {
				status, raw := e.mcp(tok, "", callTool(name, withDevice(args, target)))
				assert.True(t, isDeviceRefusal(status, raw), "%s/%s on %s: %d %s", name, cred, target, status, raw)
			}
			// Even with Alice's own number in the header, device_id names the target.
			status, raw := e.mcp(tok, "d_alice", callTool(name, withDevice(args, "d_bob")))
			assert.True(t, isDeviceRefusal(status, raw), "%s/%s header d_alice, device_id d_bob: %d %s", name, cred, status, raw)
		}
	}
	// Bob's number is exactly as it was.
	owner, err := e.rt.Store.OwnerOf(ctx, "d_bob")
	require.NoError(t, err)
	assert.Equal(t, e.bob.ID, owner)
	labels, err := e.rt.Store.LabelsOf(ctx, e.bob.ID)
	require.NoError(t, err)
	assert.Equal(t, "", labels["d_bob"])
	_, mine := e.mcp(e.bobKey, "", callTool(mcpauth.MyDevicesTool, nil))
	assert.Contains(t, mine, "d_bob")

	// Control: on her own number no tool says "device not found" (unlink last).
	names := []string{}
	for name := range numberTools {
		if name != mcpauth.UnlinkNumberTool {
			names = append(names, name)
		}
	}
	for _, name := range append(names, mcpauth.UnlinkNumberTool) {
		status, raw := e.mcp(e.aliceKey, "", callTool(name, withDevice(numberTools[name], "d_alice")))
		assert.Equal(t, http.StatusOK, status, "%s: %s", name, raw)
		assert.NotContains(t, raw, "device not found", "%s own number: %s", name, raw)
	}
}

func TestMCPAccountToolsShowOnlyTheCallersOwn(t *testing.T) {
	e := newP2(t)
	ctx := context.Background()
	_, err := e.rt.Store.CreateSession(ctx, e.bob.ID, "BobBrowser/1.0", time.Hour)
	require.NoError(t, err)
	require.NoError(t, e.rt.Store.SetLabel(ctx, e.bob.ID, "d_bob", "Bob secret label"))
	for _, name := range accountTools {
		for cred, tok := range map[string]string{"oauth": e.aliceAT, "api_key": e.aliceKey} {
			status, raw := e.mcp(tok, "", callTool(name, map[string]any{"user_id": e.bob.ID, "device_id": "d_bob"}))
			// Extra arguments are not in the schema; either refused by it or
			// ignored, never followed.
			require.Equal(t, http.StatusOK, status, "%s/%s", name, cred)
			for _, bobs := range []string{e.bob.ID, "27825550102", "d_bob", "Bob secret label", "BobBrowser", e.bobKey[:10]} {
				assert.NotContains(t, raw, bobs, "%s/%s leaks Bob's %q", name, cred, bobs)
			}
		}
	}
	_, mine := e.mcp(e.aliceKey, "", callTool(mcpauth.MyAccountTool, nil))
	assert.Contains(t, mine, "+27825550101")
	_, mine = e.mcp(e.aliceAT, "", callTool(mcpauth.MyDevicesTool, nil))
	assert.Contains(t, mine, "d_alice")
}

// Every admin tool, called by a member with an OAuth token or an API key,
// is refused and reveals nothing; Bob's account is untouched after.
func TestMCPAdminToolsRefuseMembers(t *testing.T) {
	e := newP2(t)
	ctx := context.Background()
	args := map[string]any{"user_id": e.bob.ID, "device_id": "d_bob", "phone": "+27825550102", "confirm": "+27825550102",
		"status": "disabled", "also_unlink": true, "max_numbers": 0, "action": "remove"}
	for _, name := range mcpauth.AdminTools {
		for cred, tok := range map[string]string{"oauth": e.aliceAT, "api_key": e.aliceKey, "bob_key": e.bobKey} {
			status, raw := e.mcp(tok, "", callTool(name, args))
			assert.Equal(t, http.StatusOK, status, "%s/%s", name, cred)
			assert.True(t, strings.Contains(raw, "not found") || strings.Contains(raw, mcpauth.AdminOnlyMessage), "%s/%s: %s", name, cred, raw)
			assert.NotContains(t, raw, "signin/verify", "%s/%s", name, cred)
			assert.NotContains(t, raw, `"structuredContent"`, "%s/%s", name, cred)
		}
	}
	u, err := e.rt.Store.UserByID(ctx, e.bob.ID)
	require.NoError(t, err)
	assert.Equal(t, store.StatusActive, u.Status)
	assert.Equal(t, "+27825550102", u.Phone)
	owner, _ := e.rt.Store.OwnerOf(ctx, "d_bob")
	assert.Equal(t, e.bob.ID, owner)
	// Control: the superadmin's own key reaches the same tools.
	status, raw := e.mcp(e.adminKey, "", callTool(mcpauth.AdminListUsersTool, nil))
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, raw, "+27825550102")
}

func drainBroadcasts() {
	drainHub.Do(func() {
		go func() {
			for range websocket.Broadcast {
			}
		}()
	})
}

func TestEveryRouteWithAnAPIKeyIsOwnerScoped(t *testing.T) {
	e := newP2(t)
	var checked int
	for _, r := range e.app.GetRoutes(true) {
		if r.Method == fiber.MethodHead || r.Method == fiber.MethodOptions || r.Method == fiber.MethodConnect || r.Method == fiber.MethodTrace {
			continue
		}
		if !strings.Contains(r.Path, ":device_id") && !strings.Contains(r.Path, ":id") && !isDeviceScoped(r.Path) {
			continue
		}
		if strings.HasPrefix(r.Path, "/ui/") || strings.HasPrefix(r.Path, "/account") || strings.HasPrefix(r.Path, "/admin") {
			// Page routes: a key never signs in to a page (303 to sign-in), see below.
			continue
		}
		key := r.Method + " " + r.Path
		req := newScopedRequest(r.Method, fill(r.Path, "d_bob"), key)
		req.Header.Set(middleware.DeviceIDHeader, "d_bob")
		req.Header.Set("Authorization", "Bearer "+e.aliceKey)
		resp, body := e.do(req)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, "alice's key -> bob's device via %s: %s", key, body)
		checked++
	}
	require.Greater(t, checked, 60, "the walk covered the upstream API")

	// A key never opens a page or a page action.
	for _, path := range []string{"/", "/account", "/admin/users"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+e.aliceKey)
		resp, _ := e.do(req)
		assert.Equal(t, http.StatusSeeOther, resp.StatusCode, path)
	}
}

func TestFilesAreOwnerChecked(t *testing.T) {
	e := newP2(t)
	get := func(path string, cookie bool, key string) (int, string) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie {
			req.AddCookie(e.aliceCk)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, body := e.do(req)
		return resp.StatusCode, body
	}
	for _, how := range []struct {
		cookie bool
		key    string
	}{{true, ""}, {false, e.aliceKey}} {
		status, body := get("/files/d_alice/a.jpg", how.cookie, how.key)
		assert.Equal(t, http.StatusOK, status)
		assert.Contains(t, body, "secret of d_alice")
		status, body = get("/files/d_bob/b.jpg", how.cookie, how.key)
		assert.Equal(t, http.StatusNotFound, status)
		assert.NotContains(t, body, "secret")
		status, _ = get("/files/d_alice/..%2fd_bob%2fb.jpg", how.cookie, how.key)
		assert.Equal(t, http.StatusNotFound, status)
	}
	status, _ := get("/files/d_bob/b.jpg", false, e.bobKey)
	assert.Equal(t, http.StatusOK, status, "the owner's own key fetches it")
	req := httptest.NewRequest(http.MethodGet, "/files/d_alice/a.jpg", nil)
	req.Header.Set("Authorization", "Bearer "+e.aliceAT)
	resp, _ := e.do(req)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "an OAuth token is for /mcp only")
}

func TestDisabledUserIsRefusedEverywhereAtOnce(t *testing.T) {
	e := newP2(t)
	// Works before.
	status, _ := e.mcp(e.aliceAT, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, status)

	require.NoError(t, e.rt.Store.SetUserStatus(context.Background(), e.alice.ID, store.StatusDisabled))

	for _, tok := range []string{e.aliceAT, e.aliceKey} {
		status, _ := e.mcp(tok, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		assert.Equal(t, http.StatusUnauthorized, status, "/mcp")
		// Every tool, every group of the matrix: refused before it runs.
		all := append([]string{}, accountTools...)
		all = append(all, mcpauth.AdminTools...)
		for name := range toolArgs {
			all = append(all, name)
		}
		for name := range numberTools {
			all = append(all, name)
		}
		for _, name := range all {
			status, _ := e.mcp(tok, "", callTool(name, map[string]any{"device_id": "d_alice"}))
			assert.Equal(t, http.StatusUnauthorized, status, "/mcp %s", name)
		}
	}
	for _, path := range []string{"/devices", "/files/d_alice/a.jpg"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+e.aliceKey)
		resp, _ := e.do(req)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "key on %s", path)
		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(e.aliceCk)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		resp, _ = e.do(req)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "session on %s", path)
	}
	// Refresh is refused too: covered in ui/mcp/oauth (TestInactiveSubjectCannotRedeemOrRefresh).
	status, _ = e.mcp(e.bobAT, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.Equal(t, http.StatusOK, status, "Bob is untouched")
}

func TestDisconnectOnTheAccountPageEndsTheToken(t *testing.T) {
	e := newP2(t)
	ctx := context.Background()
	grants, err := e.rt.OAuth.ListGrants(ctx, e.alice.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, "example.com", grants[0].ClientHost)

	bobID := ""
	for _, u := range mustUsers(t, e.rt) {
		if u.ID != e.alice.ID {
			bobID = u.ID
		}
	}
	bobGrants, err := e.rt.OAuth.ListGrants(ctx, bobID)
	require.NoError(t, err)
	require.Len(t, bobGrants, 1)

	post := func(family string) int {
		req := httptest.NewRequest(http.MethodPost, "/account/connections/"+family+"/revoke", strings.NewReader(url.Values{"csrf": {e.csrf}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(e.aliceCk)
		resp, _ := e.do(req)
		return resp.StatusCode
	}
	assert.Equal(t, http.StatusNotFound, post(bobGrants[0].FamilyID), "Alice cannot end Bob's connection")
	status, _ := e.mcp(e.bobAT, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.Equal(t, http.StatusOK, status)

	assert.Equal(t, http.StatusSeeOther, post(grants[0].FamilyID))
	status, _ = e.mcp(e.aliceAT, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	assert.Equal(t, http.StatusUnauthorized, status, "the next call after Disconnect fails")
}

func mustUsers(t *testing.T, rt *multiuser.Runtime) []store.User {
	t.Helper()
	us, err := rt.Store.ListUsers(context.Background())
	require.NoError(t, err)
	return us
}
