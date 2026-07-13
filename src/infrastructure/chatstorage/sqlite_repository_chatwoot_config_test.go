package chatstorage

import (
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

func TestSQLiteRepositoryChatwootDeviceConfigCRUD(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	cfg := &domainChatStorage.ChatwootDeviceConfig{
		DeviceID:    "busine",
		DeviceJID:   "628111111111@s.whatsapp.net",
		ChatwootURL: "https://chat.example.com",
		AccountID:   1,
		InboxID:     5,
		APIToken:    "tok-a",
		Enabled:     true,
	}
	if err := repo.SaveChatwootDeviceConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	if cfg.ID == 0 {
		t.Fatal("expected config ID to be populated after insert")
	}

	got, err := repo.GetChatwootDeviceConfig("busine")
	if err != nil || got == nil {
		t.Fatalf("get config: %v (got=%v)", err, got)
	}
	if got.AccountID != 1 || got.InboxID != 5 || got.APIToken != "tok-a" || !got.Enabled {
		t.Fatalf("unexpected config: %+v", got)
	}

	// Lookup by identifier resolves both device_id and device_jid.
	byID, err := repo.GetChatwootDeviceConfigByIdentifier("busine")
	if err != nil || byID == nil || byID.ID != cfg.ID {
		t.Fatalf("get by device_id: %v (%v)", err, byID)
	}
	byJID, err := repo.GetChatwootDeviceConfigByIdentifier("628111111111@s.whatsapp.net")
	if err != nil || byJID == nil || byJID.ID != cfg.ID {
		t.Fatalf("get by device_jid: %v (%v)", err, byJID)
	}

	// Update keeps the same ID and changes fields.
	cfg.InboxID = 9
	cfg.APIToken = "tok-b"
	if err := repo.SaveChatwootDeviceConfig(cfg); err != nil {
		t.Fatalf("update config: %v", err)
	}
	got, _ = repo.GetChatwootDeviceConfig("busine")
	if got.InboxID != 9 || got.APIToken != "tok-b" || got.ID != cfg.ID {
		t.Fatalf("expected updated config keeping ID, got %+v", got)
	}

	count, err := repo.CountChatwootDeviceConfigs()
	if err != nil || count != 1 {
		t.Fatalf("count = %d err=%v, want 1", count, err)
	}

	list, err := repo.ListChatwootDeviceConfigs()
	if err != nil || len(list) != 1 {
		t.Fatalf("list len = %d err=%v, want 1", len(list), err)
	}

	if err := repo.DeleteChatwootDeviceConfig("busine"); err != nil {
		t.Fatalf("delete config: %v", err)
	}
	got, _ = repo.GetChatwootDeviceConfig("busine")
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
}

// One row's device_id can collide with another row's device_jid (each column
// is only unique on its own). The identifier lookup must surface that as an
// error instead of picking a query-plan-dependent winner.
func TestSQLiteRepositoryChatwootDeviceConfigByIdentifierCollision(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	// Device "busine" paired as 628111...; another device was (unwisely) named
	// with that same JID string as its user-facing id.
	for _, cfg := range []*domainChatStorage.ChatwootDeviceConfig{
		{DeviceID: "busine", DeviceJID: "628111111111@s.whatsapp.net", ChatwootURL: "https://a.example.com", AccountID: 1, InboxID: 1, APIToken: "t", Enabled: true},
		{DeviceID: "628111111111@s.whatsapp.net", ChatwootURL: "https://b.example.com", AccountID: 2, InboxID: 2, APIToken: "t", Enabled: true},
	} {
		if err := repo.SaveChatwootDeviceConfig(cfg); err != nil {
			t.Fatalf("save %s: %v", cfg.DeviceID, err)
		}
	}

	if _, err := repo.GetChatwootDeviceConfigByIdentifier("628111111111@s.whatsapp.net"); err == nil {
		t.Fatal("colliding identifier must error, not silently pick a config")
	}

	// The unambiguous identifier still resolves.
	got, err := repo.GetChatwootDeviceConfigByIdentifier("busine")
	if err != nil || got == nil || got.DeviceID != "busine" {
		t.Fatalf("unambiguous lookup = %+v err=%v", got, err)
	}

	// A row matching by both its own device_id and device_jid is NOT ambiguous.
	self := &domainChatStorage.ChatwootDeviceConfig{
		DeviceID: "629000000000@s.whatsapp.net", DeviceJID: "629000000000@s.whatsapp.net",
		ChatwootURL: "https://c.example.com", AccountID: 3, InboxID: 3, APIToken: "t", Enabled: true,
	}
	if err := repo.SaveChatwootDeviceConfig(self); err != nil {
		t.Fatalf("save self: %v", err)
	}
	got, err = repo.GetChatwootDeviceConfigByIdentifier("629000000000@s.whatsapp.net")
	if err != nil || got == nil || got.ID != self.ID {
		t.Fatalf("self-matching lookup = %+v err=%v", got, err)
	}
}

func TestSQLiteRepositoryChatwootDeviceConfigByInboxAmbiguity(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	// Two devices on the SAME account+inbox but DIFFERENT Chatwoot URLs.
	// (UNIQUE(url,account,inbox) permits this; the inbox lookup must treat it
	// as ambiguous and return nil rather than guessing.)
	for i, url := range []string{"https://a.example.com", "https://b.example.com"} {
		if err := repo.SaveChatwootDeviceConfig(&domainChatStorage.ChatwootDeviceConfig{
			DeviceID:    []string{"dev-a", "dev-b"}[i],
			ChatwootURL: url,
			AccountID:   1,
			InboxID:     2,
			APIToken:    "tok",
			Enabled:     true,
		}); err != nil {
			t.Fatalf("save %s: %v", url, err)
		}
	}
	got, err := repo.GetChatwootDeviceConfigByInbox(1, 2)
	if err != nil {
		t.Fatalf("get by inbox: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for ambiguous inbox match, got %+v", got)
	}

	// A unique account+inbox resolves to exactly one config.
	if err := repo.SaveChatwootDeviceConfig(&domainChatStorage.ChatwootDeviceConfig{
		DeviceID: "dev-c", ChatwootURL: "https://c.example.com", AccountID: 7, InboxID: 3, APIToken: "tok", Enabled: true,
	}); err != nil {
		t.Fatalf("save dev-c: %v", err)
	}
	got, err = repo.GetChatwootDeviceConfigByInbox(7, 3)
	if err != nil || got == nil || got.DeviceID != "dev-c" {
		t.Fatalf("unique inbox lookup = %+v err=%v, want dev-c", got, err)
	}
}

func TestSQLiteRepositoryChatwootMessageLinkScopeColumns(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	link := &domainChatStorage.ChatwootMessageLink{
		DeviceID:               "dev-a",
		WhatsAppMessageID:      "wa-1",
		WhatsAppChatJID:        "628@s.whatsapp.net",
		ChatwootMessageID:      10,
		ChatwootConversationID: 50,
		ChatwootInboxID:        5,
		ChatwootConfigID:       42,
		ChatwootAccountID:      1,
		Direction:              "incoming",
	}
	if err := repo.UpsertChatwootMessageLink(link); err != nil {
		t.Fatalf("upsert link: %v", err)
	}
	got, err := repo.GetChatwootMessageLinkByWhatsAppID("dev-a", "wa-1")
	if err != nil || got == nil {
		t.Fatalf("get link: %v (%v)", err, got)
	}
	if got.ChatwootConfigID != 42 || got.ChatwootAccountID != 1 {
		t.Fatalf("scope columns not persisted: %+v", got)
	}

	n, err := repo.CountChatwootMessageLinksByConfig(42)
	if err != nil || n != 1 {
		t.Fatalf("count links by config = %d err=%v, want 1", n, err)
	}
}

func TestSQLiteRepositoryConversationLookupIsAccountScoped(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	// Same conversation_id (5) in two different Chatwoot accounts.
	for _, l := range []*domainChatStorage.ChatwootMessageLink{
		{DeviceID: "dev-a", WhatsAppMessageID: "a1", WhatsAppChatJID: "111@s.whatsapp.net", ChatwootConversationID: 5, ChatwootAccountID: 1, Direction: "incoming"},
		{DeviceID: "dev-b", WhatsAppMessageID: "b1", WhatsAppChatJID: "222@s.whatsapp.net", ChatwootConversationID: 5, ChatwootAccountID: 2, Direction: "incoming"},
	} {
		if err := repo.UpsertChatwootMessageLink(l); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	got, err := repo.GetLatestChatwootMessageLinkByConversation(5, 2, false, 0)
	if err != nil || got == nil {
		t.Fatalf("scoped lookup: %v (%v)", err, got)
	}
	if got.DeviceID != "dev-b" {
		t.Fatalf("account-scoped lookup returned wrong device: %+v", got)
	}
}

// Two separate Chatwoot servers can collide on (conversation_id, account_id) —
// fresh installs all start at account 1, conversation 1 — so per-device
// (forced-route) callers additionally scope the lookup by their own config id.
func TestSQLiteRepositoryConversationLookupIsConfigScoped(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	for _, l := range []*domainChatStorage.ChatwootMessageLink{
		{DeviceID: "dev-a", WhatsAppMessageID: "a1", WhatsAppChatJID: "111@s.whatsapp.net", ChatwootConversationID: 1, ChatwootAccountID: 1, ChatwootConfigID: 10, Direction: "incoming"},
		{DeviceID: "dev-b", WhatsAppMessageID: "b1", WhatsAppChatJID: "222@s.whatsapp.net", ChatwootConversationID: 1, ChatwootAccountID: 1, ChatwootConfigID: 20, Direction: "incoming"},
	} {
		if err := repo.UpsertChatwootMessageLink(l); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	got, err := repo.GetLatestChatwootMessageLinkByConversation(1, 1, false, 10)
	if err != nil || got == nil || got.DeviceID != "dev-a" {
		t.Fatalf("config-scoped lookup (10) = %+v err=%v, want dev-a", got, err)
	}
	got, err = repo.GetLatestChatwootMessageLinkByConversation(1, 1, false, 20)
	if err != nil || got == nil || got.DeviceID != "dev-b" {
		t.Fatalf("config-scoped lookup (20) = %+v err=%v, want dev-b", got, err)
	}
	// configID 0 keeps the account-wide behavior (shared/legacy endpoint).
	got, err = repo.GetLatestChatwootMessageLinkByConversation(1, 1, false, 0)
	if err != nil || got == nil {
		t.Fatalf("unscoped lookup: %v (%v)", err, got)
	}
}

func TestSQLiteRepositoryUpdateChatwootDeviceConfigJID(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	// Config created before the device paired: empty JID.
	cfg := &domainChatStorage.ChatwootDeviceConfig{
		DeviceID: "busine", ChatwootURL: "https://chat.example.com",
		AccountID: 1, InboxID: 5, APIToken: "tok", Enabled: true,
	}
	if err := repo.SaveChatwootDeviceConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got, _ := repo.GetChatwootDeviceConfigByIdentifier("628@s.whatsapp.net"); got != nil {
		t.Fatalf("precondition: JID must not resolve before stamping, got %+v", got)
	}

	// Device pairs: connect handler stamps the JID; the forward path (keyed by
	// JID) must now resolve the config.
	changed, err := repo.UpdateChatwootDeviceConfigJID("busine", "628@s.whatsapp.net")
	if err != nil || !changed {
		t.Fatalf("stamp JID: changed=%v err=%v", changed, err)
	}
	got, err := repo.GetChatwootDeviceConfigByIdentifier("628@s.whatsapp.net")
	if err != nil || got == nil || got.ID != cfg.ID {
		t.Fatalf("JID lookup after stamp = %+v err=%v", got, err)
	}

	// Idempotent: same JID again reports no change.
	if changed, err := repo.UpdateChatwootDeviceConfigJID("busine", "628@s.whatsapp.net"); err != nil || changed {
		t.Fatalf("re-stamp: changed=%v err=%v, want false,nil", changed, err)
	}

	// Re-pair with a new number: the stale JID is replaced.
	if changed, err := repo.UpdateChatwootDeviceConfigJID("busine", "629@s.whatsapp.net"); err != nil || !changed {
		t.Fatalf("re-pair stamp: changed=%v err=%v", changed, err)
	}
	if got, _ := repo.GetChatwootDeviceConfigByIdentifier("628@s.whatsapp.net"); got != nil {
		t.Fatalf("stale JID must no longer resolve, got %+v", got)
	}
	if got, _ := repo.GetChatwootDeviceConfigByIdentifier("629@s.whatsapp.net"); got == nil {
		t.Fatal("new JID must resolve after re-pair")
	}

	// No row / empty input: no-op.
	if changed, err := repo.UpdateChatwootDeviceConfigJID("ghost", "630@s.whatsapp.net"); err != nil || changed {
		t.Fatalf("unknown device: changed=%v err=%v, want false,nil", changed, err)
	}
}

func TestSQLiteRepositoryDeleteChatwootMessageLinksByConfig(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	for _, l := range []*domainChatStorage.ChatwootMessageLink{
		{DeviceID: "dev-a", WhatsAppMessageID: "a1", WhatsAppChatJID: "111@s.whatsapp.net", ChatwootConversationID: 1, ChatwootAccountID: 1, ChatwootConfigID: 10, Direction: "incoming"},
		{DeviceID: "dev-a", WhatsAppMessageID: "a2", WhatsAppChatJID: "111@s.whatsapp.net", ChatwootConversationID: 1, ChatwootAccountID: 1, ChatwootConfigID: 10, Direction: "outgoing"},
		{DeviceID: "dev-b", WhatsAppMessageID: "b1", WhatsAppChatJID: "222@s.whatsapp.net", ChatwootConversationID: 2, ChatwootAccountID: 1, ChatwootConfigID: 20, Direction: "incoming"},
		{DeviceID: "dev-l", WhatsAppMessageID: "l1", WhatsAppChatJID: "333@s.whatsapp.net", ChatwootConversationID: 3, ChatwootAccountID: 1, ChatwootConfigID: 0, Direction: "incoming"},
	} {
		if err := repo.UpsertChatwootMessageLink(l); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	if err := repo.DeleteChatwootMessageLinksByConfig(10); err != nil {
		t.Fatalf("delete by config: %v", err)
	}
	if n, _ := repo.CountChatwootMessageLinksByConfig(10); n != 0 {
		t.Fatalf("config 10 links remaining: %d", n)
	}
	if n, _ := repo.CountChatwootMessageLinksByConfig(20); n != 1 {
		t.Fatalf("config 20 links = %d, want 1 (untouched)", n)
	}

	// Legacy links (config 0) are never bulk-deleted.
	if err := repo.DeleteChatwootMessageLinksByConfig(0); err == nil {
		t.Fatal("deleting config-0 links must be refused")
	}
	if got, _ := repo.GetChatwootMessageLinkByWhatsAppID("dev-l", "l1"); got == nil {
		t.Fatal("legacy link must survive")
	}
}
