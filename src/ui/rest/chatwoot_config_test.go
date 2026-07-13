package rest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/fiber/v2"
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
// the config handlers use; the rest is the embedded (nil) interface.
type fakeConfigStore struct {
	domainChatStorage.IChatStorageRepository
	configs            map[string]*domainChatStorage.ChatwootDeviceConfig
	linkCount          map[int64]int
	nextID             int64
	deletedLinkConfigs []int64
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
	for _, cfg := range f.configs {
		if cfg.DeviceID == identifier || (cfg.DeviceJID != "" && cfg.DeviceJID == identifier) {
			clone := *cfg
			return &clone, nil
		}
	}
	return nil, nil
}

func (f *fakeConfigStore) ListChatwootDeviceConfigs() ([]*domainChatStorage.ChatwootDeviceConfig, error) {
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
	return nil
}

func (f *fakeConfigStore) CountChatwootDeviceConfigs() (int, error) {
	return len(f.configs), nil
}

func newConfigTestApp(t *testing.T, store *fakeConfigStore) *fiber.App {
	t.Helper()
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	dm.AddDevice(whatsapp.NewDeviceInstance("dev", nil, nil))
	chatwoot.InitClientRegistry(store)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	h := &ChatwootHandler{DeviceManager: dm, ChatStorageRepo: store}
	app := fiber.New()
	app.Get("/chatwoot/configs", h.ListChatwootConfigs)
	app.Get("/devices/:device_id/chatwoot/config", h.GetChatwootConfig)
	app.Put("/devices/:device_id/chatwoot/config", h.UpsertChatwootConfig)
	app.Delete("/devices/:device_id/chatwoot/config", h.DeleteChatwootConfig)
	return app
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
