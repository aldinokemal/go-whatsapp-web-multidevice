package chatwoot

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// fakeConfigRepo implements just the methods ClientRegistry uses; the rest of
// IChatStorageRepository is satisfied by the embedded (nil) interface.
type fakeConfigRepo struct {
	domainChatStorage.IChatStorageRepository
	byIdentifier map[string]*domainChatStorage.ChatwootDeviceConfig
	byInbox      map[[2]int]*domainChatStorage.ChatwootDeviceConfig
	count        int
}

func (f *fakeConfigRepo) GetChatwootDeviceConfigByIdentifier(identifier string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	return f.byIdentifier[identifier], nil
}

func (f *fakeConfigRepo) GetChatwootDeviceConfigByInbox(accountID, inboxID int) (*domainChatStorage.ChatwootDeviceConfig, error) {
	return f.byInbox[[2]int{accountID, inboxID}], nil
}

func (f *fakeConfigRepo) CountChatwootDeviceConfigs() (int, error) {
	return f.count, nil
}

func TestClientRegistryResolveByDeviceIDAndJID(t *testing.T) {
	cfg := &domainChatStorage.ChatwootDeviceConfig{
		ID: 7, DeviceID: "busine", DeviceJID: "628@s.whatsapp.net",
		ChatwootURL: "https://chat.example.com", AccountID: 3, InboxID: 9,
		APIToken: "tok", Enabled: true,
	}
	repo := &fakeConfigRepo{
		byIdentifier: map[string]*domainChatStorage.ChatwootDeviceConfig{
			"busine":             cfg,
			"628@s.whatsapp.net": cfg,
		},
		count: 1,
	}
	reg := NewClientRegistry(repo)

	for _, id := range []string{"busine", "628@s.whatsapp.net"} {
		rc, err := reg.Resolve(id)
		if err != nil || rc == nil || rc.Client == nil {
			t.Fatalf("resolve %q: rc=%v err=%v", id, rc, err)
		}
		if rc.ConfigID != 7 || rc.Client.AccountID != 3 || rc.Client.InboxID != 9 || rc.Client.BaseURL != "https://chat.example.com" {
			t.Fatalf("resolve %q wrong: %+v / %+v", id, rc, rc.Client)
		}
	}
}

func TestClientRegistryDisabledConfigReturnsNil(t *testing.T) {
	repo := &fakeConfigRepo{
		byIdentifier: map[string]*domainChatStorage.ChatwootDeviceConfig{
			"d": {ID: 1, DeviceID: "d", Enabled: false, AccountID: 1, InboxID: 1, ChatwootURL: "https://x.example.com", APIToken: "t"},
		},
		count: 1,
	}
	reg := NewClientRegistry(repo)
	rc, err := reg.Resolve("d")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rc != nil {
		t.Fatalf("disabled config must resolve to nil (skip), got %+v", rc)
	}
}

func TestClientRegistryEmptyTableFallsBackToEnv(t *testing.T) {
	origURL, origTok, origAcc, origInbox := config.ChatwootURL, config.ChatwootAPIToken, config.ChatwootAccountID, config.ChatwootInboxID
	t.Cleanup(func() {
		config.ChatwootURL, config.ChatwootAPIToken, config.ChatwootAccountID, config.ChatwootInboxID = origURL, origTok, origAcc, origInbox
	})
	config.ChatwootURL = "https://env.example.com"
	config.ChatwootAPIToken = "env-tok"
	config.ChatwootAccountID = 1
	config.ChatwootInboxID = 2

	repo := &fakeConfigRepo{count: 0} // empty table
	reg := NewClientRegistry(repo)
	rc, err := reg.Resolve("anything")
	if err != nil || rc == nil || rc.Client == nil {
		t.Fatalf("legacy resolve: rc=%v err=%v", rc, err)
	}
	if rc.ConfigID != 0 {
		t.Fatalf("legacy config must have ConfigID 0, got %d", rc.ConfigID)
	}
	if rc.Client.BaseURL != "https://env.example.com" || rc.Client.AccountID != 1 || rc.Client.InboxID != 2 {
		t.Fatalf("legacy client not built from env: %+v", rc.Client)
	}
}

func TestClientRegistryNonEmptyNoMatchFailsFast(t *testing.T) {
	repo := &fakeConfigRepo{count: 2} // table has configs, but none match "ghost"
	reg := NewClientRegistry(repo)
	rc, err := reg.Resolve("ghost")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if rc != nil {
		t.Fatalf("unmapped device with non-empty table must fail-fast (nil), got %+v", rc)
	}
}

func TestClientRegistryInvalidateRefreshes(t *testing.T) {
	cfg := &domainChatStorage.ChatwootDeviceConfig{ID: 1, DeviceID: "d", ChatwootURL: "https://a.example.com", AccountID: 1, InboxID: 1, APIToken: "t", Enabled: true}
	repo := &fakeConfigRepo{
		byIdentifier: map[string]*domainChatStorage.ChatwootDeviceConfig{"d": cfg},
		count:        1,
	}
	reg := NewClientRegistry(repo)

	rc, _ := reg.Resolve("d")
	if rc.Client.InboxID != 1 {
		t.Fatalf("initial inbox = %d, want 1", rc.Client.InboxID)
	}
	// Change underlying config, then invalidate: next resolve must reflect it.
	cfg.InboxID = 99
	reg.Invalidate("d")
	rc, _ = reg.Resolve("d")
	if rc.Client.InboxID != 99 {
		t.Fatalf("after invalidate inbox = %d, want 99 (stale cache)", rc.Client.InboxID)
	}
}

// A device that resolved to the env fallback while the table was empty must
// not keep that client after the first per-device config is written. The
// forward path caches under the JID (with DeviceID = JID), so the write-path
// Invalidate(<user-facing id>) can only clear it by purging legacy entries
// wholesale — this is the regression test for that purge.
func TestClientRegistryInvalidatePurgesEnvFallbackEntries(t *testing.T) {
	origURL, origTok, origAcc, origInbox := config.ChatwootURL, config.ChatwootAPIToken, config.ChatwootAccountID, config.ChatwootInboxID
	t.Cleanup(func() {
		config.ChatwootURL, config.ChatwootAPIToken, config.ChatwootAccountID, config.ChatwootInboxID = origURL, origTok, origAcc, origInbox
	})
	config.ChatwootURL = "https://env.example.com"
	config.ChatwootAPIToken = "env-tok"
	config.ChatwootAccountID = 1
	config.ChatwootInboxID = 2

	repo := &fakeConfigRepo{byIdentifier: map[string]*domainChatStorage.ChatwootDeviceConfig{}}
	reg := NewClientRegistry(repo)

	// Table empty: the forward path (keyed by JID) resolves the env fallback.
	rc, err := reg.Resolve("628@s.whatsapp.net")
	if err != nil || rc == nil || rc.ConfigID != 0 {
		t.Fatalf("precondition: env fallback expected, got rc=%+v err=%v", rc, err)
	}

	// Operator creates this device's config via PUT (user-facing id "busine");
	// the handler invalidates by that id only.
	cfg := &domainChatStorage.ChatwootDeviceConfig{
		ID: 7, DeviceID: "busine", DeviceJID: "628@s.whatsapp.net",
		ChatwootURL: "https://device.example.com", AccountID: 3, InboxID: 9,
		APIToken: "tok", Enabled: true,
	}
	repo.byIdentifier["busine"] = cfg
	repo.byIdentifier["628@s.whatsapp.net"] = cfg
	repo.count = 1
	reg.Invalidate("busine")

	// The same device's next forward must use its own config, not the env inbox.
	rc, err = reg.Resolve("628@s.whatsapp.net")
	if err != nil || rc == nil {
		t.Fatalf("resolve after config create: rc=%v err=%v", rc, err)
	}
	if rc.ConfigID != 7 || rc.Client.BaseURL != "https://device.example.com" {
		t.Fatalf("stale env fallback survived Invalidate: %+v / %+v", rc, rc.Client)
	}

	// And an unrelated device that had also cached the env fallback fails fast.
	reg2 := NewClientRegistry(&fakeConfigRepo{byIdentifier: map[string]*domainChatStorage.ChatwootDeviceConfig{}})
	if rc, _ := reg2.Resolve("999@s.whatsapp.net"); rc == nil || rc.ConfigID != 0 {
		t.Fatalf("precondition: env fallback expected for unrelated device")
	}
	reg2.repo.(*fakeConfigRepo).count = 1
	reg2.Invalidate("busine")
	if rc, _ := reg2.Resolve("999@s.whatsapp.net"); rc != nil {
		t.Fatalf("unmapped device must fail-fast after first config write, got %+v", rc)
	}
}

func TestClientRegistryResolveByInbox(t *testing.T) {
	cfg := &domainChatStorage.ChatwootDeviceConfig{ID: 5, DeviceID: "d", ChatwootURL: "https://a.example.com", AccountID: 3, InboxID: 9, APIToken: "t", Enabled: true}
	repo := &fakeConfigRepo{byInbox: map[[2]int]*domainChatStorage.ChatwootDeviceConfig{{3, 9}: cfg}}
	reg := NewClientRegistry(repo)

	rc, err := reg.ResolveByInbox(3, 9)
	if err != nil || rc == nil || rc.ConfigID != 5 || rc.Client.InboxID != 9 {
		t.Fatalf("resolve by inbox: rc=%v err=%v", rc, err)
	}
	if rc, _ := reg.ResolveByInbox(1, 1); rc != nil {
		t.Fatalf("unknown inbox must resolve nil, got %+v", rc)
	}
}
