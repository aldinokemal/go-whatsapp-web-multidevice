package chatwoot

import (
	"database/sql"
	"path/filepath"
	"testing"

	domainChatwoot "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
)

func newTestConfigRepo(t *testing.T) *DeviceConfigRepository {
	t.Helper()

	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "chatstorage.db"))
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &DeviceConfigRepository{db: db}
	if err := repo.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Migrate must be idempotent.
	if err := repo.Migrate(); err != nil {
		t.Fatalf("migrate (second call): %v", err)
	}
	return repo
}

func sampleConfig() *domainChatwoot.DeviceConfig {
	return &domainChatwoot.DeviceConfig{
		DeviceID:       "628111@s.whatsapp.net",
		ChatwootURL:    "https://chatwoot.example.com",
		APIToken:       "token-a",
		AccountID:      2,
		InboxID:        67,
		Enabled:        true,
		ImportMessages: false,
		DaysLimit:      3,
	}
}

func TestDeviceConfigSaveAndGetByDeviceID(t *testing.T) {
	repo := newTestConfigRepo(t)
	cfg := sampleConfig()

	if err := repo.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.GetByDeviceID(cfg.DeviceID)
	if err != nil {
		t.Fatalf("get by device id: %v", err)
	}
	if got == nil {
		t.Fatal("expected config, got nil")
	}
	if got.InboxID != 67 || got.AccountID != 2 || got.APIToken != "token-a" || !got.Enabled {
		t.Fatalf("unexpected config: %+v", got)
	}
}

func TestDeviceConfigGetByDeviceIDMissing(t *testing.T) {
	repo := newTestConfigRepo(t)
	got, err := repo.GetByDeviceID("missing@s.whatsapp.net")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestDeviceConfigSaveUpsert(t *testing.T) {
	repo := newTestConfigRepo(t)
	cfg := sampleConfig()
	if err := repo.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Same device_id with different inbox should update, not duplicate.
	cfg.InboxID = 99
	cfg.APIToken = "token-b"
	if err := repo.Save(cfg); err != nil {
		t.Fatalf("save (update): %v", err)
	}

	all, err := repo.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 config after upsert, got %d", len(all))
	}
	if all[0].InboxID != 99 || all[0].APIToken != "token-b" {
		t.Fatalf("upsert did not update fields: %+v", all[0])
	}
}

func TestDeviceConfigGetByInboxID(t *testing.T) {
	repo := newTestConfigRepo(t)
	if err := repo.Save(sampleConfig()); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Disabled config bound to another inbox must be ignored by lookup.
	disabled := sampleConfig()
	disabled.DeviceID = "628222@s.whatsapp.net"
	disabled.InboxID = 68
	disabled.Enabled = false
	if err := repo.Save(disabled); err != nil {
		t.Fatalf("save disabled: %v", err)
	}

	got, err := repo.GetByInboxID(2, 67)
	if err != nil {
		t.Fatalf("get by inbox: %v", err)
	}
	if got == nil || got.DeviceID != "628111@s.whatsapp.net" {
		t.Fatalf("unexpected config for inbox 67: %+v", got)
	}

	// Disabled inbox returns nothing.
	got, err = repo.GetByInboxID(2, 68)
	if err != nil {
		t.Fatalf("get by inbox (disabled): %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for disabled inbox, got %+v", got)
	}
}

func TestDeviceConfigDelete(t *testing.T) {
	repo := newTestConfigRepo(t)
	cfg := sampleConfig()
	if err := repo.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := repo.Delete(cfg.DeviceID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := repo.GetByDeviceID(cfg.DeviceID)
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}
	// Deleting a missing row is not an error.
	if err := repo.Delete("missing@s.whatsapp.net"); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}
