package whatsapp

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	_ "github.com/lib/pq"
	"go.mau.fi/util/random"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// GOWA_TEST_POSTGRES_URI points at a disposable Postgres database; the test drops
// every whatsmeow table in it first.
func testPostgres(t *testing.T) *sql.DB {
	t.Helper()
	uri := os.Getenv("GOWA_TEST_POSTGRES_URI")
	if uri == "" {
		t.Skip("GOWA_TEST_POSTGRES_URI not set")
	}
	db, err := sql.Open("postgres", uri)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DO $$ DECLARE t text; BEGIN
		FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = current_schema() AND tablename LIKE 'whatsmeow_%' LOOP
			EXECUTE format('DROP TABLE IF EXISTS %I CASCADE', t);
		END LOOP; END $$`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateSQLiteSessionsToPostgres(t *testing.T) {
	ctx := context.Background()
	pg := testPostgres(t)
	log := waLog.Noop

	// A legacy SQLite store with one paired device and one identity key.
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "whatsapp.db")
	legacy, err := sqlstore.New(ctx, sqlite.DriverName, sqlite.FormatChatStorageURI("file:"+legacyPath, true, true), log)
	if err != nil {
		t.Fatal(err)
	}
	device := legacy.NewDevice()
	jid := types.NewJID("5511900000000", types.DefaultUserServer)
	jid.Device = 12
	device.ID = &jid
	device.PushName = "Teste"
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte("details"),
		AccountSignature:    random.Bytes(64),
		AccountSignatureKey: random.Bytes(32),
		DeviceSignature:     random.Bytes(64),
	}
	if err := legacy.PutDevice(ctx, device); err != nil {
		t.Fatal(err)
	}
	var identity [32]byte
	copy(identity[:], random.Bytes(32))
	if err := device.Identities.PutIdentity(ctx, "5511911111111.0:0", identity); err != nil {
		t.Fatal(err)
	}
	// App state rows: mutation MACs reference their version row, and "mutation_macs"
	// sorts before "version" — the order that broke on real data.
	var hash [128]byte
	if err := device.AppState.PutAppStateVersion(ctx, "regular", 7, hash); err != nil {
		t.Fatal(err)
	}
	indexMAC, valueMAC := random.Bytes(32), random.Bytes(32)
	if err := device.AppState.PutAppStateMutationMACs(ctx, "regular", 7, []store.AppStateMutationMAC{{IndexMAC: indexMAC, ValueMAC: valueMAC}}); err != nil {
		t.Fatal(err)
	}
	_ = legacy.Close()

	// The Postgres store as InitWaDB leaves it: schema upgraded, no device.
	target := sqlstore.NewWithDB(pg, "postgres", log)
	if err := target.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}

	moved, err := MigrateSQLiteSessionsToPostgres(ctx, legacyPath, pg, log)
	if err != nil {
		t.Fatal(err)
	}
	if moved != 1 {
		t.Fatalf("moved %d devices, want 1", moved)
	}

	got, err := target.GetAllDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID.String() != jid.String() || got[0].PushName != "Teste" {
		t.Fatalf("unexpected devices after migration: %+v", got)
	}
	if !bytes.Equal(got[0].NoiseKey.Priv[:], device.NoiseKey.Priv[:]) || !bytes.Equal(got[0].IdentityKey.Priv[:], device.IdentityKey.Priv[:]) {
		t.Fatal("session keys changed in transit")
	}
	trusted, err := got[0].Identities.IsTrustedIdentity(ctx, "5511911111111.0:0", identity)
	if err != nil || !trusted {
		t.Fatalf("identity key not carried over (trusted=%v, err=%v)", trusted, err)
	}

	if got, err := got[0].AppState.GetAppStateMutationMAC(ctx, "regular", indexMAC); err != nil || !bytes.Equal(got, valueMAC) {
		t.Fatalf("app state mutation MAC not carried over (err %v)", err)
	}

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("legacy sqlite file should have been renamed")
	}
	if _, err := os.Stat(legacyPath + MigratedSuffix); err != nil {
		t.Fatalf("renamed legacy file missing: %v", err)
	}

	// A second boot finds nothing to import and changes nothing.
	again, err := MigrateSQLiteSessionsToPostgres(ctx, legacyPath, pg, log)
	if err != nil || again != 0 {
		t.Fatalf("second run moved %d (err %v), want 0", again, err)
	}
}

func TestMigrateSkipsWhenPostgresAlreadyHasSessions(t *testing.T) {
	ctx := context.Background()
	pg := testPostgres(t)
	log := waLog.Noop

	target := sqlstore.NewWithDB(pg, "postgres", log)
	if err := target.Upgrade(ctx); err != nil {
		t.Fatal(err)
	}
	existing := target.NewDevice()
	jid := types.NewJID("5511922222222", types.DefaultUserServer)
	existing.ID = &jid
	existing.Account = &waAdv.ADVSignedDeviceIdentity{
		Details: []byte("d"), AccountSignature: random.Bytes(64), AccountSignatureKey: random.Bytes(32), DeviceSignature: random.Bytes(64),
	}
	if err := target.PutDevice(ctx, existing); err != nil {
		t.Fatal(err)
	}

	legacyPath := filepath.Join(t.TempDir(), "whatsapp.db")
	if err := os.WriteFile(legacyPath, []byte("not even sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	moved, err := MigrateSQLiteSessionsToPostgres(ctx, legacyPath, pg, log)
	if err != nil || moved != 0 {
		t.Fatalf("moved %d (err %v), want 0 and the legacy file untouched", moved, err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatal("legacy file must stay where it was")
	}
}
