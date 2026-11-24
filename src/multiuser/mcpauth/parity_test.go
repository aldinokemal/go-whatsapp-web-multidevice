package mcpauth

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/capacity"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/web"
	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const adminPhone = "+27825550100"

// drainHub stands in for the websocket hub the server runs: the device
// usecase broadcasts on unlink and would block without a reader.
var drainHub sync.Once

// memWebhooks keeps webhook configs in memory in place of chat storage.
type memWebhooks struct {
	domainDevice.IDeviceUsecase
	mu  sync.Mutex
	cfg map[string]*chatstorage.DeviceWebhookConfig
}

func (m *memWebhooks) GetDeviceWebhookConfig(_ context.Context, id string) (*chatstorage.DeviceWebhookConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg[id], nil
}

func (m *memWebhooks) SetDeviceWebhookConfig(_ context.Context, id string, c *chatstorage.DeviceWebhookConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg[id] = c
	return nil
}

// parityEnv is /mcp mounted as the runtime mounts it, with every WTF tool:
// admin is a superadmin, Alice and Bob are members, each holding an API key.
// Alice owns d_alice and d_spare, Bob owns d_bob, the admin owns d_admin.
type parityEnv struct {
	*harness
	deps                       web.Deps
	admin, alice, bob          *store.User
	adminKey, aliceKey, bobKey string
	deleted                    *[]string
	hooks                      *memWebhooks
}

func newParityEnv(t *testing.T) *parityEnv {
	t.Helper()
	drainHub.Do(func() {
		go func() {
			for range websocket.Broadcast {
			}
		}()
	})
	h := newHarnessWith(t, oauth.Config{}, map[string]bool{adminPhone: true})
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	ctx := context.Background()
	for _, id := range []string{"d_alice", "d_spare", "d_bob", "d_admin"} {
		_, err := dm.CreateDevice(ctx, id)
		require.NoError(t, err)
	}
	hooks := &memWebhooks{IDeviceUsecase: usecase.NewDeviceService(dm, nil), cfg: map[string]*chatstorage.DeviceWebhookConfig{}}
	devices := owners.NewScopedDevices(hooks, h.st)
	var mu sync.Mutex
	deleted := []string{}
	deps := web.Deps{Store: h.st, Auth: h.am, Devices: devices, PublicURL: testPublic, ProductName: "WTF",
		Capacity: capacity.Service{Store: h.st, MaxPhones: 10}, RetentionHours: 48, Grants: h.srv, MCPURL: testResource,
		DeleteAccount: func(_ context.Context, id string) error {
			mu.Lock()
			defer mu.Unlock()
			deleted = append(deleted, id)
			return nil
		}}
	h.app.Use("/mcp", Middleware(h.srv, h.am, h.st))
	uimcp.Register(h.app, dm, uimcp.Deps{ResolveDevice: owners.NewResolver(dm, h.st).Resolve, ExtraTools: Tools(deps), ToolFilter: ToolFilter(deps)})

	e := &parityEnv{harness: h, deps: deps, deleted: &deleted, hooks: hooks}
	mk := func(phone string, devs ...string) (*store.User, string) {
		u, _ := h.user(phone)
		for _, d := range devs {
			require.NoError(t, h.st.SetOwner(ctx, d, u.ID))
		}
		key, _, err := h.st.CreateAPIKey(ctx, u.ID, "laptop")
		require.NoError(t, err)
		return u, key
	}
	e.admin, e.adminKey = mk(adminPhone, "d_admin")
	e.alice, e.aliceKey = mk("+27825550101", "d_alice", "d_spare")
	e.bob, e.bobKey = mk("+27825550102", "d_bob")
	return e
}

// call runs one tool and returns (isError, text, structured content as JSON).
func (e *parityEnv) call(key, tool string, args map[string]any) (bool, string, string) {
	e.t.Helper()
	r := (&mcpEnv{harness: e.harness}).rpc(mcpRequest(key, "", toolCall(tool, args)))
	isErr, text := toolOutcome(e.t, r)
	sc, _ := json.Marshal(r.body["result"].(map[string]any)["structuredContent"])
	return isErr, text, string(sc)
}

func (e *parityEnv) okCall(key, tool string, args map[string]any) string {
	e.t.Helper()
	isErr, text, sc := e.call(key, tool, args)
	require.False(e.t, isErr, "%s: %s", tool, text)
	return sc
}

func (e *parityEnv) refused(key, tool string, args map[string]any) string {
	e.t.Helper()
	isErr, text, _ := e.call(key, tool, args)
	require.True(e.t, isErr, "%s should be refused", tool)
	return text
}

func (e *parityEnv) toolNames(key string) []string {
	e.t.Helper()
	r := (&mcpEnv{harness: e.harness}).rpc(mcpRequest(key, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	var names []string
	for _, tl := range r.body["result"].(map[string]any)["tools"].([]any) {
		names = append(names, tl.(map[string]any)["name"].(string))
	}
	return names
}

// ---------------------------------------------------------------- numbers

func TestNumberStatusShowsMyNumbersOnly(t *testing.T) {
	e := newParityEnv(t)
	sc := e.okCall(e.aliceKey, NumberStatusTool, nil)
	assert.Contains(t, sc, `"d_alice"`)
	assert.Contains(t, sc, `"d_spare"`)
	assert.Contains(t, sc, `"state":"not_linked"`)
	assert.NotContains(t, sc, "d_bob")
	sc = e.okCall(e.aliceKey, NumberStatusTool, map[string]any{"device_id": "d_spare"})
	assert.Contains(t, sc, `"device_id":"d_spare"`)
	assert.Equal(t, deviceNotFoundMessage, e.refused(e.aliceKey, NumberStatusTool, map[string]any{"device_id": "d_bob"}))
}

func TestRenameNumberNamesAndClears(t *testing.T) {
	e := newParityEnv(t)
	e.okCall(e.aliceKey, RenameNumberTool, map[string]any{"device_id": "d_alice", "label": "Work phone"})
	assert.Contains(t, e.okCall(e.aliceKey, MyDevicesTool, nil), `"label":"Work phone"`)
	assert.Equal(t, "Keep the name to 40 characters or fewer.",
		e.refused(e.aliceKey, RenameNumberTool, map[string]any{"device_id": "d_alice", "label": strings.Repeat("x", 41)}))
	e.okCall(e.aliceKey, RenameNumberTool, map[string]any{"device_id": "d_alice", "label": ""})
	assert.NotContains(t, e.okCall(e.aliceKey, MyDevicesTool, nil), "Work phone")
	assert.Equal(t, deviceNotFoundMessage, e.refused(e.aliceKey, RenameNumberTool, map[string]any{"device_id": "d_bob", "label": "mine now"}))
}

func TestWebhookToolsUseTheGuardedSave(t *testing.T) {
	e := newParityEnv(t)
	assert.Contains(t, e.okCall(e.aliceKey, GetWebhookTool, map[string]any{"device_id": "d_alice"}), `"url":""`)
	for _, bad := range []string{"http://example.com/hook", "https://127.0.0.1/hook", "https://10.0.0.1/hook"} {
		assert.Equal(t, "Use a public https:// address for the webhook.",
			e.refused(e.aliceKey, SetWebhookTool, map[string]any{"device_id": "d_alice", "url": bad}), bad)
	}
	assert.Equal(t, "Enter the address to send events to.", e.refused(e.aliceKey, SetWebhookTool, map[string]any{"device_id": "d_alice", "url": " "}))
	sc := e.okCall(e.aliceKey, SetWebhookTool, map[string]any{"device_id": "d_alice", "url": "https://1.1.1.1/hook", "secret": "s3cret-value"})
	assert.Equal(t, `{"has_secret":true,"url":"https://1.1.1.1/hook"}`, sc)
	sc = e.okCall(e.aliceKey, SetWebhookTool, map[string]any{"device_id": "d_alice", "url": "https://1.0.0.1/hook"})
	assert.Equal(t, `{"has_secret":true,"url":"https://1.0.0.1/hook"}`, sc, "no secret given keeps the old one")
	assert.Equal(t, "s3cret-value", e.hooks.cfg["d_alice"].WebhookSecret)
	sc = e.okCall(e.aliceKey, GetWebhookTool, map[string]any{"device_id": "d_alice"})
	assert.NotContains(t, sc, "s3cret-value", "the secret is write-only")
	e.okCall(e.aliceKey, RemoveWebhookTool, map[string]any{"device_id": "d_alice"})
	assert.Equal(t, `{"has_secret":false,"url":""}`, e.okCall(e.aliceKey, GetWebhookTool, map[string]any{"device_id": "d_alice"}))
	for _, tool := range []string{GetWebhookTool, SetWebhookTool, RemoveWebhookTool} {
		assert.Equal(t, deviceNotFoundMessage, e.refused(e.aliceKey, tool, map[string]any{"device_id": "d_bob", "url": "https://1.1.1.1/h"}), tool)
	}
}

func TestUnlinkNumberRemovesOnlyMine(t *testing.T) {
	e := newParityEnv(t)
	ctx := context.Background()
	assert.Equal(t, deviceNotFoundMessage, e.refused(e.aliceKey, UnlinkNumberTool, map[string]any{"device_id": "d_bob"}))
	owner, err := e.st.OwnerOf(ctx, "d_bob")
	require.NoError(t, err)
	assert.Equal(t, e.bob.ID, owner, "Bob's number is untouched")
	e.okCall(e.aliceKey, UnlinkNumberTool, map[string]any{"device_id": "d_spare"})
	assert.NotContains(t, e.okCall(e.aliceKey, MyDevicesTool, nil), "d_spare")
}

func TestReconnectNumberIsOwnerChecked(t *testing.T) {
	e := newParityEnv(t)
	assert.Equal(t, deviceNotFoundMessage, e.refused(e.aliceKey, ReconnectNumberTool, map[string]any{"device_id": "d_bob"}))
	isErr, text, _ := e.call(e.aliceKey, ReconnectNumberTool, map[string]any{"device_id": "d_alice"})
	if isErr { // the placeholder has no WhatsApp client; never a not-found
		assert.NotEqual(t, deviceNotFoundMessage, text)
	}
}

func TestLinkNumberReservesASlotWithinTheLimit(t *testing.T) {
	e := newParityEnv(t)
	ctx := context.Background()
	isErr, text, _ := e.call(e.bobKey, LinkNumberTool, nil)
	// The new slot has no WhatsApp client here, so no QR comes back, but the
	// slot is Bob's and the refusal names it.
	ids, err := e.st.DevicesOf(ctx, e.bob.ID)
	require.NoError(t, err)
	require.Len(t, ids, 2, "a slot was reserved: %v %s", isErr, text)
	if isErr {
		assert.Contains(t, text, "device_id ")
	}
	require.NoError(t, e.st.SetMaxDevices(ctx, e.bob.ID, 2))
	assert.Equal(t, web.DeviceLimitMessage, e.refused(e.bobKey, LinkNumberTool, nil))
	assert.Equal(t, deviceNotFoundMessage, e.refused(e.bobKey, LinkNumberTool, map[string]any{"device_id": "d_alice"}))
	assert.Equal(t, deviceNotFoundMessage, e.refused(e.bobKey, LinkNumberTool, map[string]any{"device_id": "d_alice", "phone": "+27825550102"}))
}

// ---------------------------------------------------------------- account

func TestAccountToolsAreMineAndReadOnly(t *testing.T) {
	e := newParityEnv(t)
	ctx := context.Background()
	_, err := e.st.CreateSession(ctx, e.alice.ID, "Mozilla/5.0 (Macintosh) Version/17 Safari/605", 1<<40)
	require.NoError(t, err)

	sc := e.okCall(e.aliceKey, MyAccountTool, nil)
	assert.Contains(t, sc, `"phone":"+27825550101"`)
	assert.Contains(t, sc, `"numbers":2`)
	assert.Contains(t, sc, `"retention_hours":48`)
	assert.Contains(t, sc, "Messages and media are deleted after 2 days.")
	assert.Contains(t, sc, `"superadmin":false`)

	sc = e.okCall(e.aliceKey, MySessionsTool, nil)
	assert.Contains(t, sc, "Safari on macOS")
	keys := e.okCall(e.aliceKey, MyAPIKeysTool, nil)
	assert.Contains(t, keys, `"name":"laptop"`)
	assert.NotContains(t, keys, e.aliceKey, "never the key itself")
	assert.Contains(t, e.okCall(e.aliceKey, MyPasskeysTool, nil), `"total":0`)
	assert.Contains(t, e.okCall(e.aliceKey, MyConnectedAppsTool, nil), `"total":0`)

	for _, tool := range []string{MyAccountTool, MySessionsTool, MyAPIKeysTool, MyPasskeysTool, MyConnectedAppsTool} {
		_, text, sc := e.call(e.aliceKey, tool, nil)
		for _, bobs := range []string{e.bob.ID, "27825550102", "d_bob"} {
			assert.NotContains(t, text+sc, bobs, tool)
		}
	}
}

func TestConnectedAppsListsMyGrants(t *testing.T) {
	e := newParityEnv(t)
	client := e.register(claudeRedirect)
	_, ck := e.user("+27825550199")
	at, _ := e.tokensFor(ck, client, claudeRedirect)
	sc := e.okCall(at, MyConnectedAppsTool, nil)
	assert.Contains(t, sc, `"total":1`)
	assert.Contains(t, sc, `"host":"claude.ai"`)
	assert.Contains(t, e.okCall(e.aliceKey, MyConnectedAppsTool, nil), `"total":0`, "Alice sees none of it")
}

// ------------------------------------------------------------------ admin

func TestAdminToolsAreHiddenAndRefusedForMembers(t *testing.T) {
	e := newParityEnv(t)
	adminSees, aliceSees := e.toolNames(e.adminKey), e.toolNames(e.aliceKey)
	for _, tool := range AdminTools {
		assert.Contains(t, adminSees, tool)
		assert.NotContains(t, aliceSees, tool)
		r := (&mcpEnv{harness: e.harness}).rpc(mcpRequest(e.aliceKey, "", toolCall(tool, memberAdminArgs(e))))
		assert.Contains(t, r.raw, "not found", tool)
		assert.NotContains(t, r.raw, "27825550102", tool)
	}
	assert.Equal(t, len(adminSees)-len(AdminTools), len(aliceSees))
	u, err := e.st.UserByID(context.Background(), e.bob.ID)
	require.NoError(t, err)
	assert.Equal(t, store.StatusActive, u.Status, "nothing a member asked for happened")
	assert.Empty(t, *e.deleted)
}

func memberAdminArgs(e *parityEnv) map[string]any {
	return map[string]any{"user_id": e.bob.ID, "device_id": "d_bob", "phone": "+27825550102",
		"confirm": "+27825550102", "status": "disabled", "max_numbers": 5, "action": "remove"}
}

// Behaviour: with the list filter out of the way, every admin tool's own
// gate still refuses a member at call time, revealing nothing.
func TestEveryAdminToolChecksTheCallerItself(t *testing.T) {
	e := newParityEnv(t)
	tools := map[string]*server.ServerTool{}
	srv := server.NewMCPServer("t", "0", server.WithToolCapabilities(true))
	Tools(e.deps)(srv)
	for name, st := range srv.ListTools() {
		tools[name] = st
	}
	alice := auth.WithUser(context.Background(), e.alice)
	for _, name := range AdminTools {
		st, ok := tools[name]
		require.True(t, ok, name)
		req := mcpg.CallToolRequest{}
		req.Params.Name = name
		req.Params.Arguments = memberAdminArgs(e)
		res, err := st.Handler(alice, req)
		require.NoError(t, err)
		require.True(t, res.IsError, name)
		require.Len(t, res.Content, 1)
		assert.Equal(t, AdminOnlyMessage, res.Content[0].(mcpg.TextContent).Text, name)
		assert.Nil(t, res.StructuredContent, name)
	}
	u, _ := e.st.UserByID(context.Background(), e.bob.ID)
	assert.Equal(t, store.StatusActive, u.Status, "nothing a member asked for happened")
	assert.Equal(t, "+27825550102", u.Phone)
	ids, _ := e.st.DevicesOf(context.Background(), e.bob.ID)
	assert.Equal(t, []string{"d_bob"}, ids)
	assert.Empty(t, *e.deleted)
}

func TestAdminUsersAddAndSigninLink(t *testing.T) {
	e := newParityEnv(t)
	sc := e.okCall(e.adminKey, AdminListUsersTool, nil)
	assert.Contains(t, sc, `"total":3`)
	assert.Contains(t, sc, `"d_bob"`)
	assert.Contains(t, e.okCall(e.adminKey, AdminListUsersTool, map[string]any{"search": "0102"}), `"total":1`)

	sc = e.okCall(e.adminKey, AdminAddUserTool, map[string]any{"phone": "+27 82 555 0177"})
	assert.Contains(t, sc, `"phone":"+27825550177"`)
	added, err := e.st.UserByPhone(context.Background(), "+27825550177")
	require.NoError(t, err)
	assert.Contains(t, sc, `"user_id":"`+added.ID+`"`)
	assert.Contains(t, sc, testPublic+"/signin/verify?t=")
	assert.Equal(t, "That number already has an account. Use Get sign-in link on its row instead.",
		e.refused(e.adminKey, AdminAddUserTool, map[string]any{"phone": "+27825550177"}))
	assert.Contains(t, e.refused(e.adminKey, AdminAddUserTool, map[string]any{"phone": "12"}), "country code")

	assert.Contains(t, e.okCall(e.adminKey, AdminSigninLinkTool, map[string]any{"user_id": e.bob.ID}), "/signin/verify?t=")
	assert.Equal(t, notFoundMessage, e.refused(e.adminKey, AdminSigninLinkTool, map[string]any{"user_id": "u_nobody"}))
}

func TestAdminDisableEnableAndAlsoUnlink(t *testing.T) {
	e := newParityEnv(t)
	ctx := context.Background()
	assert.Equal(t, "You can't disable your own account.",
		e.refused(e.adminKey, AdminSetUserStatusTool, map[string]any{"user_id": e.admin.ID, "status": "disabled"}))
	e.okCall(e.adminKey, AdminSetUserStatusTool, map[string]any{"user_id": e.bob.ID, "status": "disabled", "also_unlink": true})
	u, _ := e.st.UserByID(ctx, e.bob.ID)
	assert.Equal(t, store.StatusDisabled, u.Status)
	ids, _ := e.st.DevicesOf(ctx, e.bob.ID)
	assert.Empty(t, ids, "his numbers are unlinked")
	assert.Equal(t, 401, (&mcpEnv{harness: e.harness}).rpc(mcpRequest(e.bobKey, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)).status,
		"his key ended")
	e.okCall(e.adminKey, AdminSetUserStatusTool, map[string]any{"user_id": e.bob.ID, "status": "active"})
	u, _ = e.st.UserByID(ctx, e.bob.ID)
	assert.Equal(t, store.StatusActive, u.Status)
}

func TestAdminLimitPhoneAndUnlink(t *testing.T) {
	e := newParityEnv(t)
	ctx := context.Background()
	e.okCall(e.adminKey, AdminSetNumberLimitTool, map[string]any{"user_id": e.bob.ID, "max_numbers": 3})
	u, _ := e.st.UserByID(ctx, e.bob.ID)
	assert.Equal(t, 3, u.MaxDevices)
	e.refused(e.adminKey, AdminSetNumberLimitTool, map[string]any{"user_id": e.bob.ID, "max_numbers": 21})

	assert.Equal(t, "That number already belongs to another user.",
		e.refused(e.adminKey, AdminChangeNumberTool, map[string]any{"user_id": e.bob.ID, "phone": "+27825550101"}))
	assert.Equal(t, "That number belongs to an admin.",
		e.refused(e.adminKey, AdminChangeNumberTool, map[string]any{"user_id": e.bob.ID, "phone": adminPhone}))
	assert.Equal(t, "You can't change your own number here.",
		e.refused(e.adminKey, AdminChangeNumberTool, map[string]any{"user_id": e.admin.ID, "phone": "+27825550188"}))
	e.okCall(e.adminKey, AdminChangeNumberTool, map[string]any{"user_id": e.bob.ID, "phone": "+27825550188"})
	u, _ = e.st.UserByID(ctx, e.bob.ID)
	assert.Equal(t, "+27825550188", u.Phone)

	assert.Equal(t, deviceNotFoundMessage,
		e.refused(e.adminKey, AdminUnlinkNumberTool, map[string]any{"user_id": e.bob.ID, "device_id": "d_alice"}), "only a number the user owns")
	e.okCall(e.adminKey, AdminUnlinkNumberTool, map[string]any{"user_id": e.bob.ID, "device_id": "d_bob"})
	ids, _ := e.st.DevicesOf(ctx, e.bob.ID)
	assert.Empty(t, ids)
	owner, _ := e.st.OwnerOf(ctx, "d_alice")
	assert.Equal(t, e.alice.ID, owner)
}

func TestAdminDeleteNeedsDisabledAndTheirNumber(t *testing.T) {
	e := newParityEnv(t)
	ctx := context.Background()
	assert.Equal(t, "Disable this account first.",
		e.refused(e.adminKey, AdminDeleteUserTool, map[string]any{"user_id": e.bob.ID, "confirm": "+27825550102"}))
	require.NoError(t, e.st.SetUserStatus(ctx, e.bob.ID, store.StatusDisabled))
	for _, typed := range []string{"DELETE", "+27825550101", "0102"} {
		assert.Equal(t, "Type their number to confirm.",
			e.refused(e.adminKey, AdminDeleteUserTool, map[string]any{"user_id": e.bob.ID, "confirm": typed}), typed)
	}
	assert.Equal(t, "You can't delete your own account here.",
		e.refused(e.adminKey, AdminDeleteUserTool, map[string]any{"user_id": e.admin.ID, "confirm": adminPhone}))
	assert.Empty(t, *e.deleted)
	e.okCall(e.adminKey, AdminDeleteUserTool, map[string]any{"user_id": e.bob.ID, "confirm": "27 82 555 0102"})
	assert.Equal(t, []string{e.bob.ID}, *e.deleted)
}

func TestAdminDeleteRefusesTheSigninNumbersOwner(t *testing.T) {
	e := newParityEnv(t)
	ctx := context.Background()
	require.NoError(t, e.st.SetSetting(ctx, store.SettingServiceDeviceID, "d_bob", e.admin.ID))
	require.NoError(t, e.st.SetUserStatus(ctx, e.bob.ID, store.StatusDisabled))
	assert.Contains(t, e.refused(e.adminKey, AdminDeleteUserTool, map[string]any{"user_id": e.bob.ID, "confirm": "+27825550102"}), "sign-in number")
	assert.Empty(t, *e.deleted)
}

func TestAdminActivityReadsTheLog(t *testing.T) {
	e := newParityEnv(t)
	e.okCall(e.adminKey, AdminSetNumberLimitTool, map[string]any{"user_id": e.bob.ID, "max_numbers": 4})
	sc := e.okCall(e.adminKey, AdminActivityTool, map[string]any{"user_id": e.bob.ID})
	assert.Contains(t, sc, "link up to 4 numbers")
	assert.Contains(t, sc, `"action":"user_limit_set"`)
	assert.NotContains(t, sc, `"ip"`)
	assert.Equal(t, notFoundMessage, e.refused(e.adminKey, AdminActivityTool, map[string]any{"user_id": "u_nobody"}))
}

func TestAdminWaitlistListAndDecide(t *testing.T) {
	e := newParityEnv(t)
	ctx := context.Background()
	for _, p := range []string{"+27825550170", "+27825550171"} {
		_, _, err := e.st.JoinWaitlist(ctx, p)
		require.NoError(t, err)
	}
	sc := e.okCall(e.adminKey, AdminWaitlistTool, nil)
	assert.Contains(t, sc, `"waiting":2`)
	assert.Contains(t, sc, `"position":1`)

	e.okCall(e.adminKey, AdminWaitlistDecideTool, map[string]any{"phone": "+27825550170", "action": "decline"})
	assert.Contains(t, e.okCall(e.adminKey, AdminWaitlistTool, map[string]any{"status": "declined"}), "+27825550170")
	assert.Equal(t, "This number was declined. Move it back to waiting first.",
		e.refused(e.adminKey, AdminWaitlistDecideTool, map[string]any{"phone": "+27825550170", "action": "admit"}))
	e.okCall(e.adminKey, AdminWaitlistDecideTool, map[string]any{"phone": "27825550170", "action": "reopen"})
	sc = e.okCall(e.adminKey, AdminWaitlistDecideTool, map[string]any{"phone": "+27825550170", "action": "admit"})
	assert.Contains(t, sc, `"result":"sent-failed"`, "no sign-in number here, so the link comes back")
	assert.Contains(t, sc, "/signin/verify?t=")
	_, err := e.st.UserByPhone(ctx, "+27825550170")
	require.NoError(t, err, "their account exists")
	e.okCall(e.adminKey, AdminWaitlistDecideTool, map[string]any{"phone": "+27825550171", "action": "remove"})
	assert.Contains(t, e.okCall(e.adminKey, AdminWaitlistTool, nil), `"waiting":0`)
	assert.Equal(t, notFoundMessage, e.refused(e.adminKey, AdminWaitlistDecideTool, map[string]any{"phone": "nope", "action": "remove"}))
}

func TestAdminSettingsSigninNumberAndCapacity(t *testing.T) {
	e := newParityEnv(t)
	sc := e.okCall(e.adminKey, AdminSettingsTool, nil)
	assert.Contains(t, sc, `"signin_status":"Not set"`)
	assert.Contains(t, sc, `"capacity":{"linked":4,"max":10,"users":3,"waiting":0}`)
	assert.Equal(t, "Pick one of your own linked numbers.",
		e.refused(e.adminKey, AdminSetSigninNumberTool, map[string]any{"device_id": "d_alice"}))
	e.okCall(e.adminKey, AdminSetSigninNumberTool, map[string]any{"device_id": "d_admin"})
	assert.Contains(t, e.okCall(e.adminKey, AdminSettingsTool, nil), `"signin_device_id":"d_admin"`)
	sc = e.okCall(e.adminKey, AdminCapacityTool, nil)
	assert.Contains(t, sc, `"linked":4`)
	assert.Contains(t, sc, `"near_full":false`)
}

// The admin gate is decided at call time from the caller's current state:
// a superadmin disabled mid-session is refused on the very next call.
func TestDisabledSuperadminIsRefusedAtOnce(t *testing.T) {
	e := newParityEnv(t)
	e.okCall(e.adminKey, AdminCapacityTool, nil)
	require.NoError(t, e.st.SetUserStatus(context.Background(), e.admin.ID, store.StatusDisabled))
	r := (&mcpEnv{harness: e.harness}).rpc(mcpRequest(e.adminKey, "", toolCall(AdminCapacityTool, nil)))
	assert.Equal(t, 401, r.status)
	// And the gate itself, given a disabled superadmin in context, says no.
	u, _ := e.st.UserByID(context.Background(), e.admin.ID)
	assert.ErrorIs(t, e.deps.CheckSuperadmin(u), web.ErrNotSuperadmin)
	_, ok := auth.UserFrom(context.Background())
	assert.False(t, ok)
}
