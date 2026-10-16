package rest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
)

func TestMaskAPIToken(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"abc":               "****",
		"abcd":              "****",
		"secret-token-1234": "****1234",
	}
	for in, want := range cases {
		if got := maskAPIToken(in); got != want {
			t.Errorf("maskAPIToken(%q) = %q, want %q", in, got, want)
		}
	}
}

// fakeConfigStore is an in-memory IChatStorageRepository covering the methods
// the config handlers and a webhook reply use; the rest is the embedded (nil)
// interface. linkCount counts links by config, prefilled or written.
type fakeConfigStore struct {
	domainChatStorage.IChatStorageRepository
	configs            map[string]*domainChatStorage.ChatwootDeviceConfig
	linkCount          map[int64]int
	nextID             int64
	deletedLinkConfigs []int64
	links              []domainChatStorage.ChatwootMessageLink
	configReads        int // GetChatwootDeviceConfigByIdentifier calls
	inboxReads         int // GetChatwootDeviceConfigByInbox calls
	listReads          int // ListChatwootDeviceConfigs calls
	listErr            error
	modeChecks         int // CountChatwootDeviceConfigs calls
}

func newFakeConfigStore() *fakeConfigStore {
	return &fakeConfigStore{configs: map[string]*domainChatStorage.ChatwootDeviceConfig{}, linkCount: map[int64]int{}}
}

func (f *fakeConfigStore) SaveChatwootDeviceConfig(cfg *domainChatStorage.ChatwootDeviceConfig) error {
	if cfg.ID == 0 {
		f.nextID++
		cfg.ID = f.nextID
	}
	clone := *cfg
	f.configs[cfg.DeviceID] = &clone
	return nil
}

func (f *fakeConfigStore) GetChatwootDeviceConfig(deviceID string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	if cfg, ok := f.configs[deviceID]; ok {
		clone := *cfg
		return &clone, nil
	}
	return nil, nil
}

func (f *fakeConfigStore) GetChatwootDeviceConfigByIdentifier(identifier string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	f.configReads++
	for _, cfg := range f.configs {
		if cfg.DeviceID == identifier || (cfg.DeviceJID != "" && cfg.DeviceJID == identifier) {
			clone := *cfg
			return &clone, nil
		}
	}
	return nil, nil
}

// GetChatwootDeviceConfigByInbox returns the enabled config of the account and
// inbox, or nil when there is none or more than one, as the real query does.
func (f *fakeConfigStore) GetChatwootDeviceConfigByInbox(accountID, inboxID int) (*domainChatStorage.ChatwootDeviceConfig, error) {
	f.inboxReads++
	var match *domainChatStorage.ChatwootDeviceConfig
	for _, cfg := range f.configs {
		if cfg.Enabled && cfg.AccountID == accountID && cfg.InboxID == inboxID {
			if match != nil {
				return nil, nil
			}
			clone := *cfg
			match = &clone
		}
	}
	return match, nil
}

// ListChatwootDeviceConfigs returns every config, enabled or not, or listErr.
func (f *fakeConfigStore) ListChatwootDeviceConfigs() ([]*domainChatStorage.ChatwootDeviceConfig, error) {
	f.listReads++
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]*domainChatStorage.ChatwootDeviceConfig, 0, len(f.configs))
	for _, cfg := range f.configs {
		clone := *cfg
		out = append(out, &clone)
	}
	return out, nil
}

func (f *fakeConfigStore) DeleteChatwootDeviceConfig(deviceID string) error {
	delete(f.configs, deviceID)
	return nil
}

func (f *fakeConfigStore) CountChatwootMessageLinksByConfig(configID int64) (int, error) {
	return f.linkCount[configID], nil
}

func (f *fakeConfigStore) DeleteChatwootMessageLinksByConfig(configID int64) error {
	f.deletedLinkConfigs = append(f.deletedLinkConfigs, configID)
	delete(f.linkCount, configID)
	f.links = slices.DeleteFunc(f.links, func(link domainChatStorage.ChatwootMessageLink) bool {
		return link.ChatwootConfigID == configID
	})
	return nil
}

func (f *fakeConfigStore) CountChatwootDeviceConfigs() (int, error) {
	f.modeChecks++
	return len(f.configs), nil
}

func (f *fakeConfigStore) UpsertChatwootMessageLink(link *domainChatStorage.ChatwootMessageLink) error {
	f.links = append(f.links, *link)
	f.linkCount[link.ChatwootConfigID]++
	return nil
}

// GetLatestChatwootMessageLinkByConversation returns the newest link of the
// conversation and account, in configID unless it is 0.
func (f *fakeConfigStore) GetLatestChatwootMessageLinkByConversation(conversationID, accountID int, _ bool, configID int64) (*domainChatStorage.ChatwootMessageLink, error) {
	for i := len(f.links) - 1; i >= 0; i-- {
		link := f.links[i]
		if link.ChatwootConversationID == conversationID && link.ChatwootAccountID == accountID &&
			(configID == 0 || link.ChatwootConfigID == configID) {
			return &link, nil
		}
	}
	return nil, nil
}

// configTestSender sends every text reply as WhatsApp message WA-REPLY.
type configTestSender struct{ domainSend.ISendUsecase }

func (configTestSender) SendText(context.Context, domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	return domainSend.GenericResponse{MessageID: "WA-REPLY"}, nil
}

func newConfigTestApp(t *testing.T, store *fakeConfigStore) *fiber.App {
	t.Helper()
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	dm.AddDevice(whatsapp.NewDeviceInstance("dev", nil, nil))
	chatwoot.InitClientRegistry(store)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	h := &ChatwootHandler{SendUsecase: configTestSender{}, DeviceManager: dm, ChatStorageRepo: store}
	app := fiber.New()
	app.Get("/chatwoot/configs", h.ListChatwootConfigs)
	app.Get("/devices/:device_id/chatwoot/config", h.GetChatwootConfig)
	app.Put("/devices/:device_id/chatwoot/config", h.UpsertChatwootConfig)
	app.Delete("/devices/:device_id/chatwoot/config", h.DeleteChatwootConfig)
	app.Post("/chatwoot/webhook", h.HandleWebhook)
	app.Post("/chatwoot/webhook/:device_id", h.HandleDeviceWebhook)
	return app
}

// configTestBody is the PUT body of dev's config on one Chatwoot server.
func configTestBody(accountID, inboxID int) string {
	return fmt.Sprintf(`{"chatwoot_url":"https://203.0.113.10","account_id":%d,"inbox_id":%d,"api_token":"t"}`, accountID, inboxID)
}

// configTestReply is an agent reply in conversation 7 (account 1, inbox 5) to
// peer-a; gowaDeviceID, when set, is the contact's gowa_device_id.
func configTestReply(gowaDeviceID string) string {
	attrs := `"gowa_whatsapp_jid":"peer-a@s.whatsapp.net"`
	if gowaDeviceID != "" {
		attrs += `,"gowa_device_id":"` + gowaDeviceID + `"`
	}
	return `{"event":"message_created","message_type":"outgoing","id":42,"content":"Olá","account":{"id":1},` +
		`"conversation":{"id":7,"inbox_id":5,"meta":{"sender":{"custom_attributes":{` + attrs + `}}}}}`
}

// otherServerConfig stores the config of device dev-b on another Chatwoot
// server, bound to accountID and inboxID (dev's usual config has 1 and 5).
func otherServerConfig(accountID, inboxID int, enabled bool) func(*fakeConfigStore) {
	return func(store *fakeConfigStore) {
		_ = store.SaveChatwootDeviceConfig(&domainChatStorage.ChatwootDeviceConfig{DeviceID: "dev-b",
			ChatwootURL: "https://203.0.113.20", AccountID: accountID, InboxID: inboxID, APIToken: "t", Enabled: enabled})
	}
}

// failingConfigList makes the config list fail.
func failingConfigList(store *fakeConfigStore) { store.listErr = errors.New("config list unavailable") }

// replyLinks lists the links written after the first skip ones as
// "message@config", in order.
func replyLinks(store *fakeConfigStore, skip int) []string {
	var out []string
	for _, link := range store.links[skip:] {
		out = append(out, fmt.Sprintf("%s@%d", link.WhatsAppMessageID, link.ChatwootConfigID))
	}
	return out
}

// earlierConversationLink is an older link of conversation 7 (account 1),
// from dev to peer-a, written under configID.
func earlierConversationLink(configID int64) *domainChatStorage.ChatwootMessageLink {
	return &domainChatStorage.ChatwootMessageLink{DeviceID: "dev", WhatsAppMessageID: "WA-OLD", WhatsAppChatJID: "peer-a@s.whatsapp.net",
		ChatwootMessageID: 40, ChatwootConversationID: 7, ChatwootInboxID: 5, ChatwootAccountID: 1, ChatwootConfigID: configID}
}

func doJSON(t *testing.T, app *fiber.App, method, path, body string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestChatwootConfigCRUDFlow(t *testing.T) {
	store := newFakeConfigStore()
	app := newConfigTestApp(t, store)

	// Create.
	resp, body := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10/","account_id":1,"inbox_id":5,"api_token":"super-secret-token"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("create status = %d body=%s", resp.StatusCode, body)
	}
	stored := store.configs["dev"]
	if stored == nil || stored.APIToken != "super-secret-token" {
		t.Fatalf("token not stored raw: %+v", stored)
	}
	if stored.ChatwootURL != "https://203.0.113.10" { // canonicalized (trailing slash removed)
		t.Fatalf("url not canonicalized: %q", stored.ChatwootURL)
	}

	// Read masks the token and never leaks the raw secret.
	resp, body = doJSON(t, app, http.MethodGet, "/devices/dev/chatwoot/config", "")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("get status = %d", resp.StatusCode)
	}
	if strings.Contains(body, "super-secret-token") {
		t.Fatalf("GET leaked raw token: %s", body)
	}
	if !strings.Contains(body, "****oken") {
		t.Fatalf("GET should show masked token, got %s", body)
	}

	// Update with empty token keeps the stored secret.
	resp, body = doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":5,"enabled":false}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("update status = %d body=%s", resp.StatusCode, body)
	}
	if store.configs["dev"].APIToken != "super-secret-token" {
		t.Fatal("empty token on update must keep the stored token")
	}
	if store.configs["dev"].Enabled {
		t.Fatal("enabled=false should have been applied")
	}

	// Delete removes the config AND its message links: stale links would keep
	// winning the account-scoped reverse lookup after a delete-and-recreate
	// rebind and hijack reply destinations.
	configID := store.configs["dev"].ID
	resp, _ = doJSON(t, app, http.MethodDelete, "/devices/dev/chatwoot/config", "")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	if _, ok := store.configs["dev"]; ok {
		t.Fatal("config not deleted")
	}
	if len(store.deletedLinkConfigs) != 1 || store.deletedLinkConfigs[0] != configID {
		t.Fatalf("delete must remove the config's message links, got %v", store.deletedLinkConfigs)
	}
}

// TestChatwootConfigWriteInvalidatesRegistry proves the PR's "registry
// invalidated on write" claim end to end: after a PUT rotates the token (or a
// DELETE removes the config), the registry must hand out a freshly built
// client instead of the cached one.
func TestChatwootConfigWriteInvalidatesRegistry(t *testing.T) {
	store := newFakeConfigStore()
	app := newConfigTestApp(t, store)

	doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":5,"api_token":"token-one"}`)

	reg := chatwoot.GetClientRegistry()
	rc, err := reg.Resolve("dev")
	if err != nil || rc == nil || rc.Client == nil || rc.Client.APIToken != "token-one" {
		t.Fatalf("initial resolve: rc=%+v err=%v", rc, err)
	}

	// Rotate the token via PUT: the cached client must not survive.
	doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":5,"api_token":"token-two"}`)
	rc, err = reg.Resolve("dev")
	if err != nil || rc == nil || rc.Client == nil {
		t.Fatalf("resolve after rotation: rc=%+v err=%v", rc, err)
	}
	if rc.Client.APIToken != "token-two" {
		t.Fatalf("registry served stale client after PUT: token=%q, want token-two", rc.Client.APIToken)
	}

	// DELETE: the per-device client must be evicted (table empty again, so the
	// next resolve falls back to the env config with ConfigID 0).
	doJSON(t, app, http.MethodDelete, "/devices/dev/chatwoot/config", "")
	rc, err = reg.Resolve("dev")
	if err != nil {
		t.Fatalf("resolve after delete: %v", err)
	}
	if rc != nil && rc.ConfigID != 0 {
		t.Fatalf("registry served deleted per-device client: %+v", rc)
	}
}

// TestChatwootConfigListEndpoint smoke-tests GET /chatwoot/configs: it must
// return every config with the token masked.
func TestChatwootConfigListEndpoint(t *testing.T) {
	store := newFakeConfigStore()
	app := newConfigTestApp(t, store)

	doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":5,"api_token":"super-secret-token"}`)

	resp, body := doJSON(t, app, http.MethodGet, "/chatwoot/configs", "")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("list status = %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(body, `"device_id":"dev"`) {
		t.Fatalf("list missing config: %s", body)
	}
	if strings.Contains(body, "super-secret-token") {
		t.Fatalf("list leaked raw token: %s", body)
	}
	if !strings.Contains(body, "****oken") {
		t.Fatalf("list should show masked token: %s", body)
	}
}

// TestChatwootConfigMaskedTokenRoundTrip proves a client that echoes the masked
// token from GET back into PUT keeps the stored secret — the mask must never be
// stored as the credential itself.
func TestChatwootConfigMaskedTokenRoundTrip(t *testing.T) {
	store := newFakeConfigStore()
	app := newConfigTestApp(t, store)

	doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":5,"api_token":"super-secret-token"}`)

	// GET shows "****oken"; a naive client PUTs that value straight back.
	resp, body := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":5,"api_token":"****oken"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("masked-echo update status = %d body=%s", resp.StatusCode, body)
	}
	if got := store.configs["dev"].APIToken; got != "super-secret-token" {
		t.Fatalf("masked token was stored as the credential: %q", got)
	}
}

func TestChatwootConfigRejectsSSRFURL(t *testing.T) {
	store := newFakeConfigStore()
	app := newConfigTestApp(t, store)
	resp, _ := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"http://127.0.0.1:3000","account_id":1,"inbox_id":5,"api_token":"t"}`)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("SSRF URL status = %d, want 400", resp.StatusCode)
	}
}

func TestChatwootConfigRejectsRoutingEditWithLinks(t *testing.T) {
	store := newFakeConfigStore()
	app := newConfigTestApp(t, store)

	// Create config (id 1) and pretend it has linked conversations.
	doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":5,"api_token":"t"}`)
	store.linkCount[store.configs["dev"].ID] = 3

	// Changing the inbox (routing identity) must be rejected with 409.
	resp, body := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":9,"api_token":"t"}`)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("routing edit status = %d body=%s, want 409", resp.StatusCode, body)
	}

	// Same for the URL and account dimensions of the routing identity.
	resp, body = doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.99","account_id":1,"inbox_id":5,"api_token":"t"}`)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("url edit status = %d body=%s, want 409", resp.StatusCode, body)
	}
	resp, body = doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":2,"inbox_id":5,"api_token":"t"}`)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("account edit status = %d body=%s, want 409", resp.StatusCode, body)
	}

	// Rotating only the token (same routing identity) is allowed.
	resp, _ = doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config",
		`{"chatwoot_url":"https://203.0.113.10","account_id":1,"inbox_id":5,"api_token":"rotated"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("token rotation status = %d, want 200", resp.StatusCode)
	}
	if store.configs["dev"].APIToken != "rotated" {
		t.Fatal("token should have been rotated")
	}
}

// In per-device mode, a reply routed without a config (by gowa_device_id, or
// by an older config-0 link) is linked under its device's config, as forwarded
// messages are: the config's account and inbox can no longer change, and
// deleting the config deletes the link.
func TestChatwootConfigOwnsRepliesRoutedWithoutConfig(t *testing.T) {
	cases := []struct {
		name         string
		gowaDeviceID string // routes the reply when set; otherwise an older config-0 link does
		wantLinks    map[int64]int
		wantLeft     []string // links left after the config delete
	}{
		{"gowa_device_id override", "dev", map[int64]int{1: 1}, nil},
		{"older config-0 link", "", map[int64]int{0: 1, 1: 1}, []string{"WA-OLD"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeConfigStore()
			app := newConfigTestApp(t, store)
			if resp, body := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config", configTestBody(1, 5)); resp.StatusCode != fiber.StatusOK {
				t.Fatalf("config status = %d body=%s", resp.StatusCode, body)
			}
			if tc.gowaDeviceID == "" {
				_ = store.UpsertChatwootMessageLink(earlierConversationLink(0))
			}

			if resp, body := doJSON(t, app, http.MethodPost, "/chatwoot/webhook", configTestReply(tc.gowaDeviceID)); resp.StatusCode != fiber.StatusOK {
				t.Fatalf("webhook status = %d body=%s", resp.StatusCode, body)
			}
			if !reflect.DeepEqual(store.linkCount, tc.wantLinks) {
				t.Errorf("links by config = %v, want %v", store.linkCount, tc.wantLinks)
			}
			for _, edit := range []string{configTestBody(2, 5), configTestBody(1, 6)} {
				resp, body := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config", edit)
				if resp.StatusCode != fiber.StatusConflict || !strings.Contains(body, "CONFIG_HAS_LINKED_CONVERSATIONS") {
					t.Errorf("PUT %s = %d %s, want 409 CONFIG_HAS_LINKED_CONVERSATIONS", edit, resp.StatusCode, body)
				}
			}

			if resp, _ := doJSON(t, app, http.MethodDelete, "/devices/dev/chatwoot/config", ""); resp.StatusCode != fiber.StatusOK {
				t.Fatalf("delete status = %d", resp.StatusCode)
			}
			var left []string
			for _, link := range store.links {
				left = append(left, link.WhatsAppMessageID)
			}
			if !reflect.DeepEqual(left, tc.wantLeft) {
				t.Errorf("links left after delete = %v, want %v", left, tc.wantLeft)
			}
		})
	}
}

// A route without a config takes its device's config only in per-device mode,
// only when that config is bound to the reply's account and inbox, and only
// when no other config, enabled or not, shares them. A gowa_device_id whose
// config is bound to another account or inbox is ignored. A route with a
// config, and legacy mode, read no config, and the mode is checked once, when
// routing.
func TestChatwootWebhookReplyConfigNeedsAMatchingDeviceConfig(t *testing.T) {
	cases := []struct {
		name         string
		config       string                 // dev's config; legacy mode when empty
		setup        func(*fakeConfigStore) // runs after dev's config is saved
		gowaDeviceID string                 // routes the reply when set; otherwise an older link does
		linkConfig   int64                  // config of that older link
		wantLinks    []string
		wantReads    [3]int // configs read by device, by inbox, listed
	}{
		{"route with a config", configTestBody(1, 5), nil, "", 1, []string{"WA-REPLY@1"}, [3]int{0, 0, 0}},
		{"device config of another inbox", configTestBody(1, 6), nil, "", 0, []string{"WA-REPLY@0"}, [3]int{1, 0, 0}},
		{"device config of another account", configTestBody(2, 5), nil, "", 0, []string{"WA-REPLY@0"}, [3]int{1, 0, 0}},
		{"config-0 link, inbox shared with another server", configTestBody(1, 5), otherServerConfig(1, 5, true), "", 0, []string{"WA-REPLY@0"}, [3]int{1, 0, 1}},
		{"gowa_device_id, inbox shared with another server", configTestBody(1, 5), otherServerConfig(1, 5, true), "dev", 0, []string{"WA-REPLY@0"}, [3]int{1, 0, 1}},
		{"config-0 link, inbox shared with a disabled config", configTestBody(1, 5), otherServerConfig(1, 5, false), "", 0, []string{"WA-REPLY@0"}, [3]int{1, 0, 1}},
		{"gowa_device_id, inbox shared with a disabled config", configTestBody(1, 5), otherServerConfig(1, 5, false), "dev", 0, []string{"WA-REPLY@0"}, [3]int{1, 0, 1}},
		{"config-0 link, other config on another inbox", configTestBody(1, 5), otherServerConfig(1, 6, true), "", 0, []string{"WA-REPLY@1"}, [3]int{1, 0, 1}},
		{"gowa_device_id, other config on another account", configTestBody(1, 5), otherServerConfig(2, 5, true), "dev", 0, []string{"WA-REPLY@1"}, [3]int{1, 0, 1}},
		{"config list fails", configTestBody(1, 5), failingConfigList, "dev", 0, []string{"WA-REPLY@0"}, [3]int{1, 0, 1}},
		{"gowa_device_id of another inbox", configTestBody(1, 6), nil, "dev", 0, nil, [3]int{1, 1, 0}},
		{"gowa_device_id of another account", configTestBody(2, 5), nil, "dev", 0, nil, [3]int{1, 1, 0}},
		{"legacy gowa_device_id override", "", nil, "dev", 0, []string{"WA-REPLY@0"}, [3]int{0, 0, 0}},
		{"legacy config-0 link", "", nil, "", 0, []string{"WA-REPLY@0"}, [3]int{0, 0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeConfigStore()
			app := newConfigTestApp(t, store)
			if tc.config != "" {
				if resp, body := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config", tc.config); resp.StatusCode != fiber.StatusOK {
					t.Fatalf("config status = %d body=%s", resp.StatusCode, body)
				}
			}
			if tc.setup != nil {
				tc.setup(store)
			}
			if tc.gowaDeviceID == "" {
				_ = store.UpsertChatwootMessageLink(earlierConversationLink(tc.linkConfig))
			}
			before := len(store.links)
			store.configReads, store.inboxReads, store.listReads, store.modeChecks = 0, 0, 0, 0

			if resp, body := doJSON(t, app, http.MethodPost, "/chatwoot/webhook", configTestReply(tc.gowaDeviceID)); resp.StatusCode != fiber.StatusOK {
				t.Fatalf("webhook status = %d body=%s", resp.StatusCode, body)
			}
			if got := replyLinks(store, before); !reflect.DeepEqual(got, tc.wantLinks) {
				t.Errorf("reply links = %v, want %v", got, tc.wantLinks)
			}
			if got := [3]int{store.configReads, store.inboxReads, store.listReads}; got != tc.wantReads || store.modeChecks != 1 {
				t.Errorf("configs read by device, by inbox, listed = %v, mode checks = %d; want %v and 1", got, store.modeChecks, tc.wantReads)
			}
		})
	}
}

// Server B shares server A's account and inbox and answers its conversation 7
// on the shared webhook, naming A's device dev by gowa_device_id. That reply
// keeps config 0, whether B's config here is enabled or not (disabling it does
// not stop B's webhooks), so A's next reply in its own conversation 7, on dev's
// per-device webhook, is not routed to B's contact.
func TestChatwootReplyOfASharedInboxStaysOutOfTheDeviceConfig(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("config of B enabled=%t", enabled), func(t *testing.T) {
			store := newFakeConfigStore()
			app := newConfigTestApp(t, store)
			if resp, body := doJSON(t, app, http.MethodPut, "/devices/dev/chatwoot/config", configTestBody(1, 5)); resp.StatusCode != fiber.StatusOK {
				t.Fatalf("config status = %d body=%s", resp.StatusCode, body)
			}
			otherServerConfig(1, 5, enabled)(store)

			replyOfB := strings.Replace(configTestReply("dev"), "peer-a@", "peer-b@", 1)
			for _, post := range []struct{ path, body string }{{"/chatwoot/webhook", replyOfB}, {"/chatwoot/webhook/dev", configTestReply("")}} {
				if resp, body := doJSON(t, app, http.MethodPost, post.path, post.body); resp.StatusCode != fiber.StatusOK {
					t.Fatalf("POST %s status = %d body=%s", post.path, resp.StatusCode, body)
				}
			}
			var got []string
			for _, link := range store.links {
				got = append(got, fmt.Sprintf("%s@%d", link.WhatsAppChatJID, link.ChatwootConfigID))
			}
			if want := []string{"peer-b@s.whatsapp.net@0", "peer-a@s.whatsapp.net@1"}; !reflect.DeepEqual(got, want) {
				t.Errorf("links by chat and config = %v, want %v", got, want)
			}
		})
	}
}

// TestChatwootConfigMultiDeviceNoDeviceMiddlewareLeak verifies that in a multi-device setup
// (where DeviceManager disambiguation fails when no device id is provided),
// Chatwoot config routes registered before headerDeviceGroup are not intercepted
// by DeviceMiddleware, allowing GET /chatwoot/configs and PUT /devices/:device_id/chatwoot/config
// without X-Device-Id header or device_id query param.
func TestChatwootConfigMultiDeviceNoDeviceMiddlewareLeak(t *testing.T) {
	store := newFakeConfigStore()
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	dm.AddDevice(whatsapp.NewDeviceInstance("dev1", nil, nil))
	dm.AddDevice(whatsapp.NewDeviceInstance("dev2", nil, nil))
	chatwoot.InitClientRegistry(store)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	h := &ChatwootHandler{DeviceManager: dm, ChatStorageRepo: store}

	t.Run("routes registered before empty prefix group are not intercepted", func(t *testing.T) {
		app := fiber.New()

		// Registered before headerDeviceGroup so they are not intercepted by DeviceMiddleware
		app.Get("/chatwoot/configs", h.ListChatwootConfigs)
		app.Get("/devices/:device_id/chatwoot/config", h.GetChatwootConfig)
		app.Put("/devices/:device_id/chatwoot/config", h.UpsertChatwootConfig)
		app.Delete("/devices/:device_id/chatwoot/config", h.DeleteChatwootConfig)

		// Device-scoped operations (header-based) with empty prefix group
		headerDeviceGroup := app.Group("", middleware.DeviceMiddleware(dm))
		headerDeviceGroup.Get("/device-scoped-probe", func(c fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		// 1. GET /chatwoot/configs without X-Device-Id responds with OK, NOT DEVICE_ID_REQUIRED
		resp, body := doJSON(t, app, http.MethodGet, "/chatwoot/configs", "")
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("GET /chatwoot/configs status = %d body = %s, want %d", resp.StatusCode, body, fiber.StatusOK)
		}

		// 2. PUT /devices/dev1/chatwoot/config without X-Device-Id responds with OK, NOT DEVICE_ID_REQUIRED
		resp, body = doJSON(t, app, http.MethodPut, "/devices/dev1/chatwoot/config",
			`{"chatwoot_url":"https://203.0.113.10/","account_id":1,"inbox_id":5,"api_token":"super-secret-token"}`)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("PUT /devices/dev1/chatwoot/config status = %d body = %s, want %d", resp.StatusCode, body, fiber.StatusOK)
		}

		// 3. GET /devices/dev1/chatwoot/config without X-Device-Id responds with OK, NOT DEVICE_ID_REQUIRED
		resp, body = doJSON(t, app, http.MethodGet, "/devices/dev1/chatwoot/config", "")
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("GET /devices/dev1/chatwoot/config status = %d body = %s, want %d", resp.StatusCode, body, fiber.StatusOK)
		}

		// 4. Device-scoped probe without X-Device-Id must be intercepted by DeviceMiddleware and fail with DEVICE_ID_REQUIRED
		resp, body = doJSON(t, app, http.MethodGet, "/device-scoped-probe", "")
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("GET /device-scoped-probe status = %d body = %s, want %d", resp.StatusCode, body, fiber.StatusBadRequest)
		}
		if !strings.Contains(body, "DEVICE_ID_REQUIRED") {
			t.Fatalf("GET /device-scoped-probe body = %s, want DEVICE_ID_REQUIRED", body)
		}
	})

	t.Run("routes registered after empty prefix group mistakenly inherit middleware", func(t *testing.T) {
		app := fiber.New()

		// Buggy ordering: empty-prefix group with DeviceMiddleware registered first
		headerDeviceGroup := app.Group("", middleware.DeviceMiddleware(dm))
		headerDeviceGroup.Get("/device-scoped-probe", func(c fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})

		// Chatwoot routes registered after: Fiber v3 leaks DeviceMiddleware to subsequent routes
		app.Get("/chatwoot/configs", h.ListChatwootConfigs)

		resp, body := doJSON(t, app, http.MethodGet, "/chatwoot/configs", "")
		if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(body, "DEVICE_ID_REQUIRED") {
			t.Fatalf("expected leaked middleware to reject with DEVICE_ID_REQUIRED, got status=%d body=%s", resp.StatusCode, body)
		}
	})
}
