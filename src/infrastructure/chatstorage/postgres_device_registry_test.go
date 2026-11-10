package chatstorage

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	_ "github.com/lib/pq"
)

// GOWA_TEST_POSTGRES_URI points at a disposable Postgres database.
func registryForTest(t *testing.T) (*PostgresDeviceRegistry, domainChatStorage.IChatStorageRepository) {
	t.Helper()
	uri := os.Getenv("GOWA_TEST_POSTGRES_URI")
	if uri == "" {
		t.Skip("GOWA_TEST_POSTGRES_URI not set")
	}
	pg, err := sql.Open("postgres", uri)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pg.Close() })

	lite, err := sql.Open(sqlite.DriverName, sqlite.FormatChatStorageURI("file:"+filepath.Join(t.TempDir(), "chatstorage.db"), true, true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lite.Close() })
	base := NewStorageRepository(lite)
	if err := base.InitializeSchema(); err != nil {
		t.Fatal(err)
	}

	registry, err := NewPostgresDeviceRegistry(base, pg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pg.Exec(`TRUNCATE gowa_devices`); err != nil {
		t.Fatal(err)
	}
	return registry, base
}

func TestRegistryImportMovesRecordsAndWebhooks(t *testing.T) {
	registry, base := registryForTest(t)

	if err := base.SaveDeviceRecord(&domainChatStorage.DeviceRecord{
		DeviceID: "wa-abc", DisplayName: "Atendimento", JID: "5511900000000@s.whatsapp.net", ADJID: "5511900000000:12@s.whatsapp.net",
	}); err != nil {
		t.Fatal(err)
	}
	hook := "http://app:8080/webhooks/whatsapp/token"
	if err := base.SetDeviceWebhookConfig("wa-abc", &domainChatStorage.DeviceWebhookConfig{
		WebhookURL: &hook, WebhookSecret: "segredo", WebhookEvents: "message",
	}); err != nil {
		t.Fatal(err)
	}

	moved, err := registry.ImportFrom(base)
	if err != nil || moved != 1 {
		t.Fatalf("moved %d (err %v), want 1", moved, err)
	}

	rec, err := registry.GetDeviceRecord("wa-abc")
	if err != nil || rec == nil || rec.DisplayName != "Atendimento" || rec.ADJID != "5511900000000:12@s.whatsapp.net" {
		t.Fatalf("record not moved: %+v (err %v)", rec, err)
	}
	cfg, err := registry.GetDeviceWebhookConfig("wa-abc")
	if err != nil || cfg == nil || cfg.WebhookURL == nil || *cfg.WebhookURL != hook || cfg.WebhookSecret != "segredo" || cfg.WebhookEvents != "message" {
		t.Fatalf("webhook not moved: %+v (err %v)", cfg, err)
	}

	// The SQLite copy is gone, so a later import cannot bring removed devices back.
	left, err := base.ListDeviceRecords()
	if err != nil || len(left) != 0 {
		t.Fatalf("old registry still has %d record(s) (err %v)", len(left), err)
	}
	if again, err := registry.ImportFrom(base); err != nil || again != 0 {
		t.Fatalf("second import moved %d (err %v), want 0", again, err)
	}
}

func TestRegistryBehavesLikeTheSQLiteOne(t *testing.T) {
	registry, _ := registryForTest(t)

	for _, rec := range []*domainChatStorage.DeviceRecord{
		{DeviceID: "a", JID: "5511900000000@s.whatsapp.net", ADJID: "5511900000000:1@s.whatsapp.net"},
		{DeviceID: "b", JID: "5511900000000@s.whatsapp.net", ADJID: "5511900000000:2@s.whatsapp.net"},
	} {
		if err := registry.SaveDeviceRecord(rec); err != nil {
			t.Fatal(err)
		}
	}
	// Upsert keeps one row per id.
	if err := registry.SaveDeviceRecord(&domainChatStorage.DeviceRecord{DeviceID: "a", DisplayName: "novo", JID: "5511900000000@s.whatsapp.net", ADJID: "5511900000000:1@s.whatsapp.net"}); err != nil {
		t.Fatal(err)
	}
	all, err := registry.ListDeviceRecords()
	if err != nil || len(all) != 2 {
		t.Fatalf("got %d records (err %v), want 2", len(all), err)
	}

	// A bare number shared by two slots is ambiguous; the AD JID is exact.
	if rec, err := registry.GetDeviceRecordByJID("5511900000000@s.whatsapp.net"); err != nil || rec != nil {
		t.Fatalf("ambiguous bare JID resolved to %+v (err %v)", rec, err)
	}
	if rec, err := registry.GetDeviceRecordByJID("5511900000000:2@s.whatsapp.net"); err != nil || rec == nil || rec.DeviceID != "b" {
		t.Fatalf("AD JID resolved to %+v (err %v)", rec, err)
	}

	hook := "http://x"
	if err := registry.SetDeviceWebhookURL("ninguem", &hook); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown device: got %v, want sql.ErrNoRows", err)
	}
	if url, err := registry.GetDeviceWebhookURL("a"); err != nil || url != nil {
		t.Fatalf("no webhook set: got %v (err %v)", url, err)
	}

	if err := registry.DeleteDeviceRecord("a"); err != nil {
		t.Fatal(err)
	}
	if rec, err := registry.GetDeviceRecord("a"); err != nil || rec != nil {
		t.Fatalf("deleted record still there: %+v (err %v)", rec, err)
	}
}
