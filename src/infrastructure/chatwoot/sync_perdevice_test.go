package chatwoot

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

func TestPerDeviceSyncServicesAreDistinct(t *testing.T) {
	t.Cleanup(func() { _ = CloseAllSyncServices() })

	c1 := NewClientFromConfig("https://a.example.com", "t", 1, 1)
	c2 := NewClientFromConfig("https://b.example.com", "t", 2, 2)

	s1 := GetSyncServiceForDevice("devA", c1, nil, false)
	s2 := GetSyncServiceForDevice("devB", c2, nil, false)

	if s1 == s2 {
		t.Fatal("expected distinct sync services per device")
	}
	if s1.client != c1 || s2.client != c2 {
		t.Fatal("each sync service must hold its own device client")
	}
	// Same key returns the cached instance.
	if again := GetSyncServiceForDevice("devA", c1, nil, false); again != s1 {
		t.Fatal("GetSyncServiceForDevice should be idempotent per key")
	}
	// Per-device services must not enable direct-Postgres import; legacy may.
	if s1.allowPgImport {
		t.Fatal("per-device service must not allow pg import")
	}
	legacy := GetSyncServiceForDevice(legacySyncServiceKey, c1, nil, true)
	if !legacy.allowPgImport {
		t.Fatal("legacy service should allow pg import")
	}
	if GetDefaultSyncService() != legacy {
		t.Fatal("GetDefaultSyncService should return the legacy-keyed service")
	}
}

func TestPgImporterGatedByAllowPgImport(t *testing.T) {
	prev := config.ChatwootImportDBURI
	config.ChatwootImportDBURI = "postgresql://user:pass@localhost:5432/chatwoot"
	t.Cleanup(func() { config.ChatwootImportDBURI = prev })

	// Direct-DB import configured, but a per-device service must not use it.
	s := &SyncService{allowPgImport: false}
	imp, err := s.pgImporterForSync(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imp != nil {
		t.Fatal("per-device service must return nil importer (REST-only) even when ChatwootImportDBURI is set")
	}
}

func TestSyncServiceKeyFor(t *testing.T) {
	if k := SyncServiceKeyFor(nil); k != legacySyncServiceKey {
		t.Fatalf("nil -> %q, want legacy key", k)
	}
	if k := SyncServiceKeyFor(&ResolvedConfig{ConfigID: 0, DeviceID: "d"}); k != legacySyncServiceKey {
		t.Fatalf("legacy config -> %q, want legacy key", k)
	}
	if k := SyncServiceKeyFor(&ResolvedConfig{ConfigID: 5, DeviceID: "d"}); k != "d" {
		t.Fatalf("per-device config -> %q, want device id", k)
	}
}
