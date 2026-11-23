package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingApp is the app usecase behind the whatsapp_app tool; it records
// every device it was asked about (everything else is never called here).
type recordingApp struct {
	domainApp.IAppUsecase
	mu   sync.Mutex
	seen []string
}

func (r *recordingApp) Status(_ context.Context, deviceID string) (bool, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, deviceID)
	return true, true, nil
}

func (r *recordingApp) devices() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

type mcpEnv struct {
	*harness
	app     *recordingApp
	alice   *store.User
	aliceCk *http.Cookie
	bob     *store.User
	aliceAT string
	bobAT   string
	aliceRT string
	client  string
}

// newMCPEnv mounts /mcp the way the runtime does: the guard on /mcp first,
// then the real MCP route with the owner-checked resolver. Alice owns
// d_alice; Bob owns d_bob; d_spare is nobody's.
func newMCPEnv(t *testing.T) *mcpEnv {
	t.Helper()
	h := newHarness(t, oauth.Config{})
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	for _, id := range []string{"d_alice", "d_bob", "d_spare"} {
		_, err := dm.CreateDevice(context.Background(), id)
		require.NoError(t, err)
	}
	resolver := owners.NewResolver(dm, h.st)
	devices := owners.NewScopedDevices(usecase.NewDeviceService(dm, nil), h.st)
	rec := &recordingApp{}
	h.app.Use("/mcp", Middleware(h.srv, h.am, h.st))
	uimcp.Register(h.app, dm, uimcp.Deps{App: rec, ResolveDevice: resolver.Resolve, ExtraTools: Tools(devices)})

	alice, aliceCk := h.user("+27825550101")
	bob, bobCk := h.user("+27825550102")
	ctx := context.Background()
	require.NoError(t, h.st.SetOwner(ctx, "d_alice", alice.ID))
	require.NoError(t, h.st.SetOwner(ctx, "d_bob", bob.ID))
	client := h.register(claudeRedirect)
	aliceAT, aliceRT := h.tokensFor(aliceCk, client, claudeRedirect)
	bobAT, _ := h.tokensFor(bobCk, client, claudeRedirect)
	return &mcpEnv{harness: h, app: rec, alice: alice, aliceCk: aliceCk, bob: bob, aliceAT: aliceAT, bobAT: bobAT, aliceRT: aliceRT, client: client}
}

type rpcResult struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func (e *mcpEnv) rpc(req *http.Request) rpcResult {
	e.t.Helper()
	resp, raw := e.do(req)
	out := rpcResult{status: resp.StatusCode, header: resp.Header, raw: raw}
	_ = json.Unmarshal([]byte(raw), &out.body)
	return out
}

func mcpRequest(token, deviceHeader, body string) *http.Request {
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if deviceHeader != "" {
		req.Header.Set("X-Device-Id", deviceHeader)
	}
	return req
}

func toolCall(name string, args map[string]any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args}})
	return string(b)
}

// toolOutcome returns (isError, text) for a tools/call response.
func toolOutcome(t *testing.T, r rpcResult) (bool, string) {
	t.Helper()
	require.Equal(t, http.StatusOK, r.status, r.raw)
	res, ok := r.body["result"].(map[string]any)
	require.True(t, ok, "a tool result: %s", r.raw)
	isErr, _ := res["isError"].(bool)
	var text strings.Builder
	if content, ok := res["content"].([]any); ok {
		for _, c := range content {
			if m, ok := c.(map[string]any); ok {
				s, _ := m["text"].(string)
				text.WriteString(s)
			}
		}
	}
	return isErr, text.String()
}

func assert401WithChallenge(t *testing.T, r rpcResult) {
	t.Helper()
	assert.Equal(t, http.StatusUnauthorized, r.status, r.raw)
	challenge := strings.Join(r.header.Values("WWW-Authenticate"), ", ")
	assert.Contains(t, challenge, `Bearer resource_metadata="https://wtf.test/.well-known/oauth-protected-resource/mcp"`)
	assert.NotContains(t, challenge, "Basic")
}

func TestNoTokenIs401WithResourceMetadata(t *testing.T) {
	e := newMCPEnv(t)
	assert401WithChallenge(t, e.rpc(mcpRequest("", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	assert401WithChallenge(t, e.rpc(mcpRequest("not-a-token", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	req := mcpRequest("", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req.Header.Set("Authorization", "Basic dXNlcjpzZWNyZXQ=")
	assert401WithChallenge(t, e.rpc(req))
}

func TestMalformedBodyWithoutTokenIs401(t *testing.T) {
	e := newMCPEnv(t)
	assert401WithChallenge(t, e.rpc(mcpRequest("", "", `{"jsonrpc":`)))
}

func TestUnsupportedProtocolVersionWithoutTokenIs401(t *testing.T) {
	e := newMCPEnv(t)
	req := mcpRequest("", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req.Header.Set("MCP-Protocol-Version", "1999-01-01")
	assert401WithChallenge(t, e.rpc(req))
}

func TestCookieIsIgnoredOnMCP(t *testing.T) {
	e := newMCPEnv(t)
	req := mcpRequest("", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req.AddCookie(e.aliceCk)
	r := e.rpc(req)
	assert401WithChallenge(t, r)
	assert.Empty(t, r.header.Values("Set-Cookie"), "the session cookie is not refreshed on /mcp")
}

func TestValidTokenListsToolsIncludingMyDevices(t *testing.T) {
	e := newMCPEnv(t)
	r := e.rpc(mcpRequest(e.aliceAT, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	require.Equal(t, http.StatusOK, r.status, r.raw)
	assert.Contains(t, r.raw, `"wtf_my_devices"`)
	assert.Contains(t, r.raw, `"whatsapp_app"`)
}

func TestDisabledUsersTokenIs401(t *testing.T) {
	e := newMCPEnv(t)
	require.NoError(t, e.st.SetUserStatus(context.Background(), e.alice.ID, store.StatusDisabled))
	assert401WithChallenge(t, e.rpc(mcpRequest(e.aliceAT, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	raw, _, err := e.st.CreateAPIKey(context.Background(), e.alice.ID, "k")
	require.NoError(t, err)
	assert401WithChallenge(t, e.rpc(mcpRequest(raw, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
}

func TestRevokedGrantIs401(t *testing.T) {
	e := newMCPEnv(t)
	ctx := context.Background()
	grants, err := e.srv.ListGrants(ctx, e.alice.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.NoError(t, e.srv.RevokeGrant(ctx, e.alice.ID, grants[0].FamilyID))
	assert401WithChallenge(t, e.rpc(mcpRequest(e.aliceAT, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	r := e.rpc(mcpRequest(e.bobAT, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	assert.Equal(t, http.StatusOK, r.status, "Bob's connection is not touched")
}

func TestAPIKeyWorksOnMCP(t *testing.T) {
	e := newMCPEnv(t)
	raw, k, err := e.st.CreateAPIKey(context.Background(), e.alice.ID, "Claude Code")
	require.NoError(t, err)
	isErr, text := toolOutcome(t, e.rpc(mcpRequest(raw, "", toolCall("whatsapp_app", map[string]any{"action": "status"}))))
	assert.False(t, isErr, text)
	assert.Equal(t, []string{"d_alice"}, e.app.devices())
	require.NoError(t, e.st.RevokeAPIKey(context.Background(), e.alice.ID, k.ID))
	assert401WithChallenge(t, e.rpc(mcpRequest(raw, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	assert401WithChallenge(t, e.rpc(mcpRequest("wtfk_unknownunknownunknownunknownunknownunk", "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
}

func TestHeaderDeviceOfAnotherUserIsNotResolved(t *testing.T) {
	e := newMCPEnv(t)
	for _, target := range []string{"d_bob", "d_spare", "d_ghost"} {
		isErr, text := toolOutcome(t, e.rpc(mcpRequest(e.aliceAT, target, toolCall("whatsapp_app", map[string]any{"action": "status"}))))
		assert.True(t, isErr, "alice via X-Device-Id %s: %s", target, text)
	}
	assert.Empty(t, e.app.devices(), "the usecase never ran for a device Alice does not own")
}

func TestToolArgumentDeviceOfAnotherUserRefused(t *testing.T) {
	e := newMCPEnv(t)
	var texts []string
	for _, target := range []string{"d_bob", "d_ghost"} {
		isErr, text := toolOutcome(t, e.rpc(mcpRequest(e.aliceAT, "d_alice", toolCall("whatsapp_app", map[string]any{"action": "status", "device_id": target}))))
		assert.True(t, isErr, "alice via device_id %s: %s", target, text)
		texts = append(texts, text)
	}
	assert.Equal(t, texts[0], strings.ReplaceAll(texts[1], "d_ghost", "d_bob"), "another user's device reads like an unknown one")
	assert.Empty(t, e.app.devices())
}

func TestSingleOwnedDeviceIsTheDefault(t *testing.T) {
	e := newMCPEnv(t)
	isErr, text := toolOutcome(t, e.rpc(mcpRequest(e.aliceAT, "", toolCall("whatsapp_app", map[string]any{"action": "status"}))))
	assert.False(t, isErr, text)
	isErr, text = toolOutcome(t, e.rpc(mcpRequest(e.bobAT, "", toolCall("whatsapp_app", map[string]any{"action": "status", "device_id": "d_bob"}))))
	assert.False(t, isErr, text)
	assert.Equal(t, []string{"d_alice", "d_bob"}, e.app.devices())
}

func TestTwoOwnedDevicesNeedAnID(t *testing.T) {
	e := newMCPEnv(t)
	require.NoError(t, e.st.SetOwner(context.Background(), "d_spare", e.alice.ID))
	isErr, text := toolOutcome(t, e.rpc(mcpRequest(e.aliceAT, "", toolCall("whatsapp_app", map[string]any{"action": "status"}))))
	assert.True(t, isErr)
	assert.Contains(t, text, "device identification required")
	isErr, _ = toolOutcome(t, e.rpc(mcpRequest(e.aliceAT, "d_spare", toolCall("whatsapp_app", map[string]any{"action": "status"}))))
	assert.False(t, isErr)
	assert.Equal(t, []string{"d_spare"}, e.app.devices())
}
