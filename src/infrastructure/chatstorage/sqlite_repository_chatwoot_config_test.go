package chatstorage

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
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
	// configID 0 is the shared webhook. Two configs means two servers that
	// reused account 1 / conversation 1; the newest row must not win.
	got, err = repo.GetLatestChatwootMessageLinkByConversation(1, 1, false, 0)
	if err != nil {
		t.Fatalf("ambiguous lookup: %v", err)
	}
	if got != nil {
		t.Fatalf("ambiguous servers must not resolve, got %+v", got)
	}
}

func TestSQLiteRepositoryConversationLookupUnscopedUsesSingleConfig(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	for _, l := range []*domainChatStorage.ChatwootMessageLink{
		{DeviceID: "dev-a", WhatsAppMessageID: "old", WhatsAppChatJID: "111@s.whatsapp.net", ChatwootConversationID: 1, ChatwootAccountID: 1, ChatwootConfigID: 10, Direction: "incoming"},
		{DeviceID: "dev-a", WhatsAppMessageID: "new", WhatsAppChatJID: "111@s.whatsapp.net", ChatwootConversationID: 1, ChatwootAccountID: 1, ChatwootConfigID: 10, Direction: "incoming"},
		// A legacy zero-config row for the same conversation must not count as
		// a second server, and must not beat the real config.
		{DeviceID: "dev-stale", WhatsAppMessageID: "stale", WhatsAppChatJID: "999@s.whatsapp.net", ChatwootConversationID: 1, ChatwootAccountID: 1, ChatwootConfigID: 0, Direction: "incoming"},
	} {
		if err := repo.UpsertChatwootMessageLink(l); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	// The zero-config row is the newest. It must not beat the single real config.
	if _, err := repo.db.Exec(`UPDATE chatwoot_message_links SET updated_at = ? WHERE wa_message_id = ?`, time.Now().Add(2*time.Hour), "stale"); err != nil {
		t.Fatalf("touch stale: %v", err)
	}
	if _, err := repo.db.Exec(`UPDATE chatwoot_message_links SET updated_at = ? WHERE wa_message_id = ?`, time.Now().Add(time.Hour), "new"); err != nil {
		t.Fatalf("touch new: %v", err)
	}

	got, err := repo.GetLatestChatwootMessageLinkByConversation(1, 1, false, 0)
	if err != nil || got == nil || got.WhatsAppMessageID != "new" || got.DeviceID != "dev-a" {
		t.Fatalf("single-config lookup = %+v err=%v, want dev-a/new", got, err)
	}
}

func TestConversationLookupRecountsSecondConfigOnSameConnection(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	if err := repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
		DeviceID: "dev-a", WhatsAppMessageID: "a1", WhatsAppChatJID: "111@s.whatsapp.net",
		ChatwootConversationID: 1, ChatwootAccountID: 1, ChatwootConfigID: 10, Direction: "incoming",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	prev := chatwootLinkLookupBeforeSelect
	chatwootLinkLookupBeforeSelect = func(tx *sql.Tx) {
		now := time.Now()
		if _, err := tx.Exec(`
			INSERT INTO chatwoot_message_links (
				device_id, wa_message_id, wa_chat_jid, chatwoot_message_id,
				chatwoot_conversation_id, chatwoot_inbox_id,
				chatwoot_contact_inbox_source_id, source_id, direction,
				is_read, created_at, updated_at, chatwoot_config_id, chatwoot_account_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, "dev-b", "b1", "222@s.whatsapp.net", 2, 1, 1, "", "", "incoming", false, now, now.Add(time.Hour), 20, 1); err != nil {
			t.Errorf("insert second config: %v", err)
		}
	}
	t.Cleanup(func() { chatwootLinkLookupBeforeSelect = prev })

	got, err := repo.GetLatestChatwootMessageLinkByConversation(1, 1, false, 0)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got != nil {
		t.Fatalf("second configuration visible in the transaction must return no row, got %+v", got)
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

// TestSQLiteRepositoryChatwootDeviceConfigWebhookSecret covers the per-device
// webhook secret through insert, update and every lookup that reads a config.
func TestSQLiteRepositoryChatwootDeviceConfigWebhookSecret(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	cfg := &domainChatStorage.ChatwootDeviceConfig{
		DeviceID:      "busine",
		DeviceJID:     "628111111111@s.whatsapp.net",
		ChatwootURL:   "https://chat.example.com",
		AccountID:     1,
		InboxID:       5,
		APIToken:      "tok-a",
		Enabled:       true,
		WebhookSecret: "channel-secret",
	}
	if err := repo.SaveChatwootDeviceConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	byDevice, err := repo.GetChatwootDeviceConfig("busine")
	if err != nil || byDevice == nil || byDevice.WebhookSecret != "channel-secret" {
		t.Fatalf("get by device: %v (%+v)", err, byDevice)
	}
	byJID, err := repo.GetChatwootDeviceConfigByIdentifier("628111111111@s.whatsapp.net")
	if err != nil || byJID == nil || byJID.WebhookSecret != "channel-secret" {
		t.Fatalf("get by identifier: %v (%+v)", err, byJID)
	}
	byInbox, err := repo.GetChatwootDeviceConfigByInbox(1, 5)
	if err != nil || byInbox == nil || byInbox.WebhookSecret != "channel-secret" {
		t.Fatalf("get by inbox: %v (%+v)", err, byInbox)
	}
	all, err := repo.ListChatwootDeviceConfigs()
	if err != nil || len(all) != 1 || all[0].WebhookSecret != "channel-secret" {
		t.Fatalf("list: %v (%+v)", err, all)
	}

	cfg.WebhookSecret = ""
	if err := repo.SaveChatwootDeviceConfig(cfg); err != nil {
		t.Fatalf("update config: %v", err)
	}
	cleared, err := repo.GetChatwootDeviceConfig("busine")
	if err != nil || cleared == nil || cleared.WebhookSecret != "" {
		t.Fatalf("after clearing: %v (%+v)", err, cleared)
	}

	// A config saved without a secret (every existing row after the migration)
	// reads back empty, which means "use the global secret".
	plain := &domainChatStorage.ChatwootDeviceConfig{
		DeviceID: "other", ChatwootURL: "https://chat.example.com", AccountID: 1, InboxID: 6, APIToken: "tok-b", Enabled: true,
	}
	if err := repo.SaveChatwootDeviceConfig(plain); err != nil {
		t.Fatalf("save plain config: %v", err)
	}
	got, err := repo.GetChatwootDeviceConfig("other")
	if err != nil || got == nil || got.WebhookSecret != "" {
		t.Fatalf("plain config: %v (%+v)", err, got)
	}
}

// TestSQLiteRepositoryWebhookSecretMigrationKeepsExistingConfigs upgrades a
// database that already held device configs before the webhook_secret column
// existed: the schema is built up to the migration before it, rows are written
// with that older schema, and only then is the rest migrated.
func TestSQLiteRepositoryWebhookSecretMigrationKeepsExistingConfigs(t *testing.T) {
	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "chatstorage.db"))
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := &SQLiteRepository{db: db}

	// Locate the migration by what it does, so migrations added around it do
	// not invalidate the test.
	migrations := repo.getMigrations()
	secretMigration := -1
	for i, migration := range migrations {
		if strings.Contains(migration, "chatwoot_device_configs ADD COLUMN webhook_secret") {
			secretMigration = i
		}
	}
	if secretMigration < 0 {
		t.Fatal("webhook_secret migration not found")
	}
	if _, err := repo.getSchemaVersion(); err != nil {
		t.Fatalf("create schema_info: %v", err)
	}
	for i := 0; i < secretMigration; i++ {
		if err := repo.runMigration(migrations[i], i+1); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}

	var columns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('chatwoot_device_configs') WHERE name = 'webhook_secret'`).Scan(&columns); err != nil || columns != 0 {
		t.Fatalf("pre-change schema already has webhook_secret (count=%d err=%v)", columns, err)
	}

	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, row := range []struct {
		deviceID, deviceJID string
		inboxID             int
		enabled             bool
	}{
		{"busine", "628111111111@s.whatsapp.net", 5, true},
		{"unpaired", "", 6, false},
	} {
		if _, err := db.Exec(`
			INSERT INTO chatwoot_device_configs (
				device_id, device_jid, chatwoot_url, account_id, inbox_id,
				api_token, enabled, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, row.deviceID, row.deviceJID, "https://chat.example.com", 1, row.inboxID, "tok-"+row.deviceID, row.enabled, created, created); err != nil {
			t.Fatalf("insert pre-change row %s: %v", row.deviceID, err)
		}
	}

	// The upgrade: the same call the application makes at startup.
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if version, err := repo.getSchemaVersion(); err != nil || version != len(migrations) {
		t.Fatalf("schema version = %d err=%v, want %d", version, err, len(migrations))
	}

	all, err := repo.ListChatwootDeviceConfigs()
	if err != nil || len(all) != 2 {
		t.Fatalf("list after upgrade: %v (%d rows, want 2)", err, len(all))
	}
	busine, err := repo.GetChatwootDeviceConfigByIdentifier("628111111111@s.whatsapp.net")
	if err != nil || busine == nil {
		t.Fatalf("get upgraded row: %v (%+v)", err, busine)
	}
	if busine.DeviceID != "busine" || busine.InboxID != 5 || busine.APIToken != "tok-busine" || !busine.Enabled ||
		!busine.CreatedAt.Equal(created) || busine.WebhookSecret != "" {
		t.Fatalf("upgraded row changed or has a non-empty secret: %+v", busine)
	}
	unpaired, err := repo.GetChatwootDeviceConfig("unpaired")
	if err != nil || unpaired == nil || unpaired.Enabled || unpaired.WebhookSecret != "" {
		t.Fatalf("second upgraded row: %v (%+v)", err, unpaired)
	}

	// An upgraded row accepts a secret like a new one.
	busine.WebhookSecret = "channel-secret"
	if err := repo.SaveChatwootDeviceConfig(busine); err != nil {
		t.Fatalf("save secret on upgraded row: %v", err)
	}
	got, err := repo.GetChatwootDeviceConfig("busine")
	if err != nil || got == nil || got.WebhookSecret != "channel-secret" || got.ID != busine.ID {
		t.Fatalf("secret on upgraded row: %v (%+v)", err, got)
	}
}
