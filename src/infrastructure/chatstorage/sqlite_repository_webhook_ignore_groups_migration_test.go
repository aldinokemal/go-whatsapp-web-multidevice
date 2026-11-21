package chatstorage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
)

// A database already at main's last migration (54) must still gain
// webhook_ignore_groups on upgrade, and its existing device rows must read and
// accept the override afterwards.
func TestWebhookIgnoreGroupsMigrationFromVersion54Database(t *testing.T) {
	const mainSchemaVersion = 54

	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "v54.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := &SQLiteRepository{db: db}
	if _, err := repo.getSchemaVersion(); err != nil {
		t.Fatalf("init schema_info: %v", err)
	}
	migrations := repo.getMigrations()
	if len(migrations) <= mainSchemaVersion {
		t.Fatalf("expected migrations beyond version %d, got %d", mainSchemaVersion, len(migrations))
	}
	for i, migration := range migrations[:mainSchemaVersion] {
		if err := repo.runMigration(migration, i+1); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	var columns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name = 'webhook_ignore_groups'`).Scan(&columns); err != nil {
		t.Fatalf("read devices columns: %v", err)
	}
	if columns != 0 {
		t.Fatalf("webhook_ignore_groups must be added after version %d, not inside main's migrations", mainSchemaVersion)
	}
	const deviceJID = "5511999990001@s.whatsapp.net"
	if _, err := db.Exec(`INSERT INTO devices (device_id, jid) VALUES ('dev1', ?)`, deviceJID); err != nil {
		t.Fatalf("insert legacy device: %v", err)
	}

	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("migration must be idempotent on restart: %v", err)
	}
	version, err := repo.getSchemaVersion()
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("expected schema version %d, got %d", len(migrations), version)
	}

	ctx := context.Background()
	cfg, err := repo.GetDeviceWebhookConfig(ctx, "dev1")
	if err != nil {
		t.Fatalf("get webhook config after upgrade: %v", err)
	}
	if cfg == nil || cfg.WebhookIgnoreGroups != nil {
		t.Fatalf("expected the upgraded device to have no group override, got %+v", cfg)
	}
	ignore := true
	if err := repo.SetDeviceWebhookConfig("dev1", &domainChatStorage.DeviceWebhookConfig{
		WebhookIgnoreGroups:    &ignore,
		WebhookIgnoreGroupsSet: true,
	}); err != nil {
		t.Fatalf("set webhook config after upgrade: %v", err)
	}
	record, err := repo.GetDeviceRecordByJID(deviceJID)
	if err != nil {
		t.Fatalf("get record by jid after upgrade: %v", err)
	}
	if record == nil || record.WebhookIgnoreGroups == nil || !*record.WebhookIgnoreGroups {
		t.Fatalf("expected webhook_ignore_groups=true on the upgraded record, got %+v", record)
	}
}
