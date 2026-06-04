package whatsapp

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
)

func TestApplyKeyCacheStorePreservesPrivacyTokens(t *testing.T) {
	primaryStore := &store.NoopStore{Error: errors.New("primary")}
	keyCacheStore := &store.NoopStore{Error: errors.New("keys")}
	device := &store.Device{}
	device.SetAllStores(primaryStore)

	applyKeyCacheStore(device, keyCacheStore)

	if device.Identities != keyCacheStore {
		t.Fatal("expected identities to use key cache store")
	}
	if device.Sessions != keyCacheStore {
		t.Fatal("expected sessions to use key cache store")
	}
	if device.PreKeys != keyCacheStore {
		t.Fatal("expected prekeys to use key cache store")
	}
	if device.SenderKeys != keyCacheStore {
		t.Fatal("expected sender keys to use key cache store")
	}
	if device.MsgSecrets != keyCacheStore {
		t.Fatal("expected message secrets to use key cache store")
	}
	if device.PrivacyTokens != primaryStore {
		t.Fatal("expected privacy tokens to stay on primary store")
	}
}

func TestListDevices_SortsByCreatedAtAscending(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	// Create devices with different creation times (in random order)
	now := time.Now()
	devices := []*DeviceInstance{
		{id: "device-c", createdAt: now.Add(2 * time.Hour)},
		{id: "device-a", createdAt: now},
		{id: "device-b", createdAt: now.Add(1 * time.Hour)},
	}

	// Add in the given order (which is not sorted by createdAt)
	for _, d := range devices {
		manager.devices[d.id] = d
	}

	// Get list multiple times to verify consistent sorting
	for i := 0; i < 10; i++ {
		result := manager.ListDevices()

		// Verify order: device-a, device-b, device-c (oldest to newest)
		if len(result) != 3 {
			t.Fatalf("iteration %d: expected 3 devices, got %d", i, len(result))
		}
		if result[0].ID() != "device-a" {
			t.Errorf("iteration %d: expected first device to be device-a, got %s", i, result[0].ID())
		}
		if result[1].ID() != "device-b" {
			t.Errorf("iteration %d: expected second device to be device-b, got %s", i, result[1].ID())
		}
		if result[2].ID() != "device-c" {
			t.Errorf("iteration %d: expected third device to be device-c, got %s", i, result[2].ID())
		}
	}
}

func TestListDevices_EmptyList(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	result := manager.ListDevices()

	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d devices", len(result))
	}
}

func TestListDevices_SingleDevice(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	device := &DeviceInstance{id: "only-device", createdAt: time.Now()}
	manager.devices[device.id] = device

	result := manager.ListDevices()

	if len(result) != 1 {
		t.Fatalf("expected 1 device, got %d", len(result))
	}
	if result[0].ID() != "only-device" {
		t.Errorf("expected device id to be only-device, got %s", result[0].ID())
	}
}

func TestListDevices_SameCreatedAt(t *testing.T) {
	manager := &DeviceManager{
		devices: make(map[string]*DeviceInstance),
	}

	// Devices with same creation time should be sorted by ID as tie-breaker
	sameTime := time.Now()
	devices := []*DeviceInstance{
		{id: "device-3", createdAt: sameTime},
		{id: "device-1", createdAt: sameTime},
		{id: "device-2", createdAt: sameTime},
	}

	for _, d := range devices {
		manager.devices[d.id] = d
	}

	expectedOrder := []string{"device-1", "device-2", "device-3"}

	// Call ListDevices multiple times to verify consistent ordering
	for i := 0; i < 10; i++ {
		result := manager.ListDevices()

		if len(result) != 3 {
			t.Fatalf("iteration %d: expected 3 devices, got %d", i, len(result))
		}

		// Verify order: devices should be sorted by ID when createdAt is equal
		for j, expected := range expectedOrder {
			if result[j].ID() != expected {
				t.Errorf("iteration %d: expected device at index %d to be %s, got %s",
					i, j, expected, result[j].ID())
			}
		}
	}
}

func TestLoadExistingDevicesPreservesStoreJIDForStoreOnlyDevice(t *testing.T) {
	ctx := context.Background()
	storeContainer := newTestSQLStore(t)
	adJID := types.NewADJID("6281111111111", types.WhatsAppDomain, 12)
	device := newTestStoreDevice(storeContainer, adJID, "stored-device")
	if err := storeContainer.PutDevice(ctx, device); err != nil {
		t.Fatalf("put device: %v", err)
	}

	manager := NewDeviceManager(storeContainer, nil, nil)

	if err := manager.LoadExistingDevices(ctx); err != nil {
		t.Fatalf("load existing devices: %v", err)
	}

	nonADJID := adJID.ToNonAD().String()
	instance, ok := manager.GetDevice(nonADJID)
	if !ok {
		t.Fatalf("expected device %s to be registered", nonADJID)
	}
	if instance.JID() != nonADJID {
		t.Fatalf("expected instance JID %s, got %q", nonADJID, instance.JID())
	}
}

func TestEnsureClientReusesPersistedADStoreDeviceFromNonADID(t *testing.T) {
	ctx := context.Background()
	storeContainer := newTestSQLStore(t)
	adJID := types.NewADJID("6281222222222", types.WhatsAppDomain, 23)
	device := newTestStoreDevice(storeContainer, adJID, "stored-device")
	if err := storeContainer.PutDevice(ctx, device); err != nil {
		t.Fatalf("put device: %v", err)
	}

	manager := NewDeviceManager(storeContainer, nil, nil)
	nonADJID := adJID.ToNonAD().String()

	instance, err := manager.EnsureClient(ctx, nonADJID)
	if err != nil {
		t.Fatalf("ensure client: %v", err)
	}

	client := instance.GetClient()
	if client == nil || client.Store == nil || client.Store.ID == nil {
		t.Fatal("expected client to reuse persisted store device with a known JID")
	}
	if got := client.Store.ID.String(); got != adJID.String() {
		t.Fatalf("expected store JID %s, got %s", adJID.String(), got)
	}
	if got := instance.JID(); got != nonADJID {
		t.Fatalf("expected instance JID %s, got %q", nonADJID, got)
	}
}

func TestSyncKeysDeviceDoesNotDeleteOtherDevices(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)
	keysStore := newTestSQLStore(t)

	firstJID := types.NewADJID("6281333333333", types.WhatsAppDomain, 31)
	secondJID := types.NewADJID("6281444444444", types.WhatsAppDomain, 32)
	for _, testDevice := range []*store.Device{
		newTestStoreDevice(primaryStore, firstJID, "primary-first"),
		newTestStoreDevice(primaryStore, secondJID, "primary-second"),
		newTestStoreDevice(keysStore, firstJID, "keys-first"),
		newTestStoreDevice(keysStore, secondJID, "keys-second"),
	} {
		if err := testDevice.Save(ctx); err != nil {
			t.Fatalf("save device %s: %v", testDevice.ID.String(), err)
		}
	}

	syncKeysDevice(ctx, primaryStore, keysStore, firstJID)

	devices, err := keysStore.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("get keys devices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected keys DB to retain 2 devices, got %d", len(devices))
	}
	assertStoreHasDevice(t, devices, firstJID.String())
	assertStoreHasDevice(t, devices, secondJID.String())
}

func TestSyncKeysDeviceUsesValueEquality(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)
	keysStore := newTestSQLStore(t)
	jid := types.NewADJID("6281555555555", types.WhatsAppDomain, 44)

	primaryDevice := newTestStoreDevice(primaryStore, jid, "primary")
	keysDevice := newTestStoreDevice(keysStore, jid, "keys")
	if err := primaryDevice.Save(ctx); err != nil {
		t.Fatalf("save primary device: %v", err)
	}
	if err := keysDevice.Save(ctx); err != nil {
		t.Fatalf("save keys device: %v", err)
	}

	syncKeysDevice(ctx, primaryStore, keysStore, jid)

	storedDevice, err := keysStore.GetDevice(ctx, jid)
	if err != nil {
		t.Fatalf("get keys device: %v", err)
	}
	if storedDevice == nil {
		t.Fatal("expected keys device to remain present")
	}
	if storedDevice.PushName != "keys" {
		t.Fatalf("expected existing keys device to be preserved, got push name %q", storedDevice.PushName)
	}
}

func TestSyncKeysDeviceMatchesAcrossADAndNonADFormats(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)
	keysStore := newTestSQLStore(t)
	adJID := types.NewADJID("6281666666666", types.WhatsAppDomain, 50)
	nonADJID := adJID.ToNonAD()

	primaryDevice := newTestStoreDevice(primaryStore, adJID, "primary")
	keysDevice := newTestStoreDevice(keysStore, nonADJID, "keys")
	if err := primaryDevice.Save(ctx); err != nil {
		t.Fatalf("save primary device: %v", err)
	}
	if err := keysDevice.Save(ctx); err != nil {
		t.Fatalf("save keys device: %v", err)
	}

	syncKeysDevice(ctx, primaryStore, keysStore, adJID)

	devices, err := keysStore.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("get keys devices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected keys DB to avoid duplicating AD and non-AD JIDs, got %d devices", len(devices))
	}
}

func newTestSQLStore(t *testing.T) *sqlstore.Container {
	t.Helper()

	uri := sqlite.FormatChatStorageURI("file:"+filepath.Join(t.TempDir(), "whatsmeow.db"), true, true)
	container, err := sqlstore.New(context.Background(), sqlite.DriverName, uri, nil)
	if err != nil {
		t.Fatalf("create sqlstore: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Close()
	})
	return container
}

func newTestStoreDevice(container *sqlstore.Container, jid types.JID, pushName string) *store.Device {
	device := container.NewDevice()
	device.ID = &jid
	device.PushName = pushName
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{1},
		AccountSignature:    make([]byte, 64),
		AccountSignatureKey: make([]byte, 32),
		DeviceSignature:     make([]byte, 64),
	}
	return device
}

func assertStoreHasDevice(t *testing.T, devices []*store.Device, jid string) {
	t.Helper()
	for _, device := range devices {
		if device != nil && device.ID != nil && device.ID.String() == jid {
			return
		}
	}
	t.Fatalf("expected store to contain device %s", jid)
}
