package webhook

import (
	"database/sql"
	"path/filepath"
	"testing"

	domainWebhook "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/webhook"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
)

func newTestWebhookRepo(t *testing.T) *DeviceWebhookRepository {
	t.Helper()

	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "chatstorage.db"))
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	repo := &DeviceWebhookRepository{db: db}
	if err := repo.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Migrate must be idempotent.
	if err := repo.Migrate(); err != nil {
		t.Fatalf("migrate (second call): %v", err)
	}
	return repo
}

func sampleWebhookConfig() *domainWebhook.DeviceWebhookConfig {
	return &domainWebhook.DeviceWebhookConfig{
		DeviceID:   "628111@s.whatsapp.net",
		WebhookURL: "https://backend.example.com/webhook-personal",
		Secret:     "secret-a",
		Events:     []string{"message", "message.ack"},
		Enabled:    true,
		Headers:    map[string]string{"X-Custom": "value"},
	}
}

func TestSaveInsertsAndAssignsID(t *testing.T) {
	repo := newTestWebhookRepo(t)
	cfg := sampleWebhookConfig()

	if err := repo.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if cfg.ID == 0 {
		t.Fatal("expected Save to assign a non-zero ID on insert")
	}

	got, err := repo.GetByID(cfg.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got == nil {
		t.Fatal("expected config, got nil")
	}
	if got.WebhookURL != cfg.WebhookURL || got.Secret != "secret-a" || !got.Enabled {
		t.Fatalf("unexpected config: %+v", got)
	}
	if len(got.Events) != 2 || got.Events[0] != "message" || got.Events[1] != "message.ack" {
		t.Fatalf("unexpected events round-trip: %+v", got.Events)
	}
	if got.Headers["X-Custom"] != "value" {
		t.Fatalf("unexpected headers round-trip: %+v", got.Headers)
	}
}

func TestSaveUpdatesExistingRow(t *testing.T) {
	repo := newTestWebhookRepo(t)
	cfg := sampleWebhookConfig()
	if err := repo.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	cfg.WebhookURL = "https://backend.example.com/webhook-empresa"
	cfg.Enabled = false
	cfg.Events = nil
	cfg.Headers = nil
	if err := repo.Save(cfg); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := repo.GetByID(cfg.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got == nil || got.WebhookURL != "https://backend.example.com/webhook-empresa" || got.Enabled {
		t.Fatalf("update not applied: %+v", got)
	}
	if got.Events != nil {
		t.Fatalf("expected nil events (all), got %+v", got.Events)
	}
	if got.Headers != nil {
		t.Fatalf("expected nil headers, got %+v", got.Headers)
	}

	all, err := repo.GetAll()
	if err != nil {
		t.Fatalf("get all: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("update should not create a new row, got %d rows", len(all))
	}
}

func TestGetByDeviceIDReturnsMultiple(t *testing.T) {
	repo := newTestWebhookRepo(t)
	device := "628111@s.whatsapp.net"

	for i := 0; i < 3; i++ {
		cfg := sampleWebhookConfig()
		cfg.WebhookURL = "https://backend.example.com/hook"
		if err := repo.Save(cfg); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	// A different device should not appear in the result.
	other := sampleWebhookConfig()
	other.DeviceID = "628999@s.whatsapp.net"
	if err := repo.Save(other); err != nil {
		t.Fatalf("save other: %v", err)
	}

	got, err := repo.GetByDeviceID(device)
	if err != nil {
		t.Fatalf("get by device id: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 configs for device, got %d", len(got))
	}
}

func TestDeleteByID(t *testing.T) {
	repo := newTestWebhookRepo(t)
	cfg := sampleWebhookConfig()
	if err := repo.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := repo.Delete(cfg.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := repo.GetByID(cfg.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil after delete, got %+v", got)
	}

	// Deleting a missing id is not an error.
	if err := repo.Delete(9999); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestSaveValidatesRequiredFields(t *testing.T) {
	repo := newTestWebhookRepo(t)

	if err := repo.Save(&domainWebhook.DeviceWebhookConfig{WebhookURL: "https://x"}); err == nil {
		t.Fatal("expected error when device_id is empty")
	}
	if err := repo.Save(&domainWebhook.DeviceWebhookConfig{DeviceID: "628@s.whatsapp.net"}); err == nil {
		t.Fatal("expected error when webhook_url is empty")
	}
}
