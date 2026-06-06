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

	got, err := repo.GetLatestChatwootMessageLinkByConversation(5, 2, false)
	if err != nil || got == nil {
		t.Fatalf("scoped lookup: %v (%v)", err, got)
	}
	if got.DeviceID != "dev-b" {
		t.Fatalf("account-scoped lookup returned wrong device: %+v", got)
	}
}
