package whatsapp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	logrustest "github.com/sirupsen/logrus/hooks/test"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
)

// keepSlotStubStorage is a minimal IChatStorageRepository that records the device
// registry calls exercised by the logout/purge paths and can be told to fail, so the
// tests can assert error propagation. All other interface methods are nil (embedded)
// and must not be called by the code under test.
type keepSlotStubStorage struct {
	domainChatStorage.IChatStorageRepository
	saveErr        error
	deleteDataErr  error
	listRecordsErr error
	savedRecords   []*domainChatStorage.DeviceRecord
	deviceRecords  []*domainChatStorage.DeviceRecord
	deletedData    []string
	deletedRecords []string
}

func (s *keepSlotStubStorage) SaveDeviceRecord(rec *domainChatStorage.DeviceRecord) error {
	cloned := *rec
	s.savedRecords = append(s.savedRecords, &cloned)
	return s.saveErr
}

func (s *keepSlotStubStorage) DeleteDeviceData(deviceID string) error {
	s.deletedData = append(s.deletedData, deviceID)
	return s.deleteDataErr
}

func (s *keepSlotStubStorage) DeleteDeviceRecord(deviceID string) error {
	s.deletedRecords = append(s.deletedRecords, deviceID)
	return nil
}

// The purge path drops the device's Chatwoot config with it; these slots have
// none, so the lookup returns empty and the delete methods are never reached.
func (s *keepSlotStubStorage) GetChatwootDeviceConfig(string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	return nil, nil
}

func (s *keepSlotStubStorage) ListDeviceRecords() ([]*domainChatStorage.DeviceRecord, error) {
	return s.deviceRecords, s.listRecordsErr
}

// assertStoreLacksJID fails if any device row in the container still matches the given
// NonAD JID. Matching mirrors deleteStoreRowsForJID / LoadExistingDevices.
func assertStoreLacksJID(t *testing.T, ctx context.Context, c *sqlstore.Container, nonADJID string) {
	t.Helper()
	devices, err := c.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("get all devices: %v", err)
	}
	for _, d := range devices {
		if d != nil && d.ID != nil && d.ID.ToNonAD().String() == nonADJID {
			t.Fatalf("expected store to no longer contain jid %s", nonADJID)
		}
	}
}

// Scenario: keep-slot logout for a slot that was loaded from storage with NO live
// client (the case aldinokemal flagged at device_manager.go:240). The orphan whatsmeow
// rows must be deleted by JID from BOTH the primary and the separate keys container,
// while the slot itself (id + display name) is preserved with an empty persisted JID.
// The slot id is deliberately different from the JID to prove matching is by JID, not
// by the slot id (the latent AD-JID-vs-slot-id mismatch in the old code).
func TestLogoutDeviceKeepSlot_NoClientDeletesStoreRowsByJIDKeepsSlot(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)
	keysStore := newTestSQLStore(t)

	adJID := types.NewADJID("6281999999991", types.WhatsAppDomain, 10)
	nonAD := adJID.ToNonAD().String()
	if err := newTestStoreDevice(primaryStore, adJID, "primary").Save(ctx); err != nil {
		t.Fatalf("save primary device: %v", err)
	}
	if err := newTestStoreDevice(keysStore, adJID, "keys").Save(ctx); err != nil {
		t.Fatalf("save keys device: %v", err)
	}

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(primaryStore, keysStore, storage)

	const slotID = "slot-uuid-1" // intentionally != JID
	inst := &DeviceInstance{id: slotID, jid: nonAD, displayName: "tIAtendo", createdAt: time.Now()}
	manager.devices[slotID] = inst

	if err := manager.LogoutDeviceKeepSlot(ctx, slotID); err != nil {
		t.Fatalf("LogoutDeviceKeepSlot returned error: %v", err)
	}

	assertStoreLacksJID(t, ctx, primaryStore, nonAD)
	assertStoreLacksJID(t, ctx, keysStore, nonAD)

	if _, ok := manager.GetDevice(slotID); !ok {
		t.Fatal("expected slot to be kept after logout")
	}
	if got := inst.JID(); got != "" {
		t.Fatalf("expected instance JID cleared after logout, got %q", got)
	}
	if len(storage.savedRecords) == 0 {
		t.Fatal("expected SaveDeviceRecord to persist the kept slot")
	}
	last := storage.savedRecords[len(storage.savedRecords)-1]
	if last.DeviceID != slotID || last.JID != "" {
		t.Fatalf("expected persisted slot %s with empty JID, got %s / %q", slotID, last.DeviceID, last.JID)
	}
}

// Scenario: DELETE purge removes the whatsmeow rows by JID from both containers even
// when the slot id differs from the JID (the old code compared the AD JID string to the
// slot id and never matched, so the rows were never deleted), and removes the slot.
func TestPurgeDevice_DeletesStoreRowsByJIDAcrossPrimaryAndKeys(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)
	keysStore := newTestSQLStore(t)

	adJID := types.NewADJID("6281999999992", types.WhatsAppDomain, 11)
	nonAD := adJID.ToNonAD().String()
	if err := newTestStoreDevice(primaryStore, adJID, "primary").Save(ctx); err != nil {
		t.Fatalf("save primary device: %v", err)
	}
	if err := newTestStoreDevice(keysStore, adJID, "keys").Save(ctx); err != nil {
		t.Fatalf("save keys device: %v", err)
	}

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(primaryStore, keysStore, storage)

	const slotID = "slot-uuid-2"
	manager.devices[slotID] = &DeviceInstance{id: slotID, jid: nonAD, displayName: "tIAtendo", createdAt: time.Now()}

	if err := manager.PurgeDevice(ctx, slotID); err != nil {
		t.Fatalf("PurgeDevice returned error: %v", err)
	}

	assertStoreLacksJID(t, ctx, primaryStore, nonAD)
	assertStoreLacksJID(t, ctx, keysStore, nonAD)

	if _, ok := manager.GetDevice(slotID); ok {
		t.Fatal("expected slot to be removed after purge")
	}
	if len(storage.deletedData) == 0 || storage.deletedData[0] != slotID {
		t.Fatalf("expected chatstorage data deletion for %s, got %v", slotID, storage.deletedData)
	}
	if len(storage.deletedRecords) == 0 || storage.deletedRecords[0] != slotID {
		t.Fatalf("expected device record deletion for %s, got %v", slotID, storage.deletedRecords)
	}
}

// resetDeviceKeepSlot must surface a persistence failure instead of silently
// succeeding (coderabbitai device_manager.go:252-276), and LogoutDeviceKeepSlot must
// combine that error into its return value.
func TestResetDeviceKeepSlot_PropagatesSaveError(t *testing.T) {
	ctx := context.Background()
	storage := &keepSlotStubStorage{saveErr: errors.New("disk full")}
	manager := NewDeviceManager(nil, nil, storage)

	const slotID = "slot-persist-err"
	manager.devices[slotID] = &DeviceInstance{id: slotID, jid: "6281999999993@s.whatsapp.net", createdAt: time.Now()}

	if err := manager.resetDeviceKeepSlot(slotID); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected resetDeviceKeepSlot to propagate save error, got %v", err)
	}

	if err := manager.LogoutDeviceKeepSlot(ctx, slotID); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("expected LogoutDeviceKeepSlot to surface the persistence error, got %v", err)
	}
}

// DELETE promises a real purge: a LOCAL cleanup failure (chatstorage/store/keys) must
// be surfaced, not masked as success (aldinokemal/coderabbitai usecase device.go:68).
func TestPurgeDevice_SurfacesLocalCleanupFailure(t *testing.T) {
	ctx := context.Background()
	storage := &keepSlotStubStorage{deleteDataErr: errors.New("chatstorage down")}
	manager := NewDeviceManager(nil, nil, storage)

	const slotID = "slot-cleanup-err"
	manager.devices[slotID] = &DeviceInstance{id: slotID, createdAt: time.Now()}

	err := manager.PurgeDevice(ctx, slotID)
	if err == nil || !strings.Contains(err.Error(), "chatstorage down") {
		t.Fatalf("expected PurgeDevice to surface the local cleanup failure, got %v", err)
	}
	if _, ok := manager.GetDevice(slotID); !ok {
		t.Fatal("expected slot to remain registered after failed purge so DELETE can be retried")
	}
}

func TestPurgeDevice_RetryAfterTransientCleanupFailure(t *testing.T) {
	ctx := context.Background()
	storage := &keepSlotStubStorage{deleteDataErr: errors.New("chatstorage down")}
	manager := NewDeviceManager(nil, nil, storage)

	const slotID = "slot-retry-cleanup"
	manager.devices[slotID] = &DeviceInstance{id: slotID, createdAt: time.Now()}

	if err := manager.PurgeDevice(ctx, slotID); err == nil {
		t.Fatal("expected first purge to fail")
	}
	storage.deleteDataErr = nil
	if err := manager.PurgeDevice(ctx, slotID); err != nil {
		t.Fatalf("expected second purge to succeed, got %v", err)
	}
	if _, ok := manager.GetDevice(slotID); ok {
		t.Fatal("expected slot removed after successful retry")
	}
}

func TestPurgeDevice_ExactSlotIDDoesNotTrimToSibling(t *testing.T) {
	ctx := context.Background()
	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(nil, nil, storage)

	const salesID = "sales"
	const spacedID = " sales "
	manager.devices[salesID] = &DeviceInstance{id: salesID, createdAt: time.Now()}
	manager.devices[spacedID] = &DeviceInstance{id: spacedID, createdAt: time.Now()}

	if err := manager.PurgeDevice(ctx, spacedID); err != nil {
		t.Fatalf("PurgeDevice returned error: %v", err)
	}
	if _, ok := manager.GetDevice(salesID); !ok {
		t.Fatal("expected sibling slot sales to remain when purging exact id \" sales \"")
	}
	if _, ok := manager.GetDevice(spacedID); ok {
		t.Fatal("expected spaced slot to be removed")
	}
}

func TestPurgeDevice_DoesNotDeleteSharedJIDPartitionWhenSiblingSlotExists(t *testing.T) {
	ctx := context.Background()
	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(nil, nil, storage)

	const slotA = "companion-a"
	const slotB = "companion-b"
	sharedJID := "6281777000021@s.whatsapp.net"
	manager.devices[slotA] = &DeviceInstance{id: slotA, jid: sharedJID, createdAt: time.Now()}
	manager.devices[slotB] = &DeviceInstance{id: slotB, jid: sharedJID, createdAt: time.Now()}
	storage.deviceRecords = []*domainChatStorage.DeviceRecord{
		{DeviceID: slotA, JID: sharedJID},
		{DeviceID: slotB, JID: sharedJID},
	}

	if err := manager.PurgeDevice(ctx, slotA); err != nil {
		t.Fatalf("PurgeDevice returned error: %v", err)
	}
	for _, key := range storage.deletedData {
		if key == sharedJID {
			t.Fatalf("expected shared JID partition %q not to be deleted while sibling slot remains, got deletes %v", sharedJID, storage.deletedData)
		}
	}
	if _, ok := manager.GetDevice(slotB); !ok {
		t.Fatal("expected sibling slot to remain in registry")
	}
}

func TestPurgeDevice_RequiresExistingSlot(t *testing.T) {
	manager := NewDeviceManager(nil, nil, nil)
	if err := manager.PurgeDevice(context.Background(), "missing-slot"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected PurgeDevice to fail for missing slot, got %v", err)
	}
}

func TestPurgeDevice_ResolvesByJID(t *testing.T) {
	ctx := context.Background()
	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(nil, nil, storage)

	const slotID = "slot-resolve-jid"
	nonAD := "6281999999999@s.whatsapp.net"
	manager.devices[slotID] = &DeviceInstance{id: slotID, jid: nonAD, createdAt: time.Now()}

	if err := manager.PurgeDevice(ctx, nonAD); err != nil {
		t.Fatalf("PurgeDevice returned error: %v", err)
	}
	if _, ok := manager.GetDevice(slotID); ok {
		t.Fatal("expected slot to be removed when purging by JID")
	}
	if len(storage.deletedRecords) == 0 || storage.deletedRecords[0] != slotID {
		t.Fatalf("expected device record deletion for %s, got %v", slotID, storage.deletedRecords)
	}
}

func TestPurgeDevice_RemovesSlotEvenWhenLogoutWouldKeepIt(t *testing.T) {
	ctx := context.Background()
	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(nil, nil, storage)

	const slotID = "slot-purge-over-keep"
	inst := &DeviceInstance{id: slotID, jid: "6281888888888@s.whatsapp.net", createdAt: time.Now()}
	inst.SetOnLoggedOut(func(deviceID string) {
		if err := manager.keepSlotLogout(ctx, deviceID); err != nil {
			t.Errorf("keepSlotLogout during purge: %v", err)
		}
	})
	manager.devices[slotID] = inst

	if err := manager.PurgeDevice(ctx, slotID); err != nil {
		t.Fatalf("PurgeDevice returned error: %v", err)
	}
	if _, ok := manager.GetDevice(slotID); ok {
		t.Fatal("expected slot to be removed even when a keep-slot callback is wired")
	}
}

// deleteStoreRowsForJID is a no-op for an empty JID: a slot that was never paired has no
// store rows, and an empty JID must never scan/delete anything.
func TestDeleteStoreRowsForJID_EmptyJIDIsNoOp(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)
	adJID := types.NewADJID("6281999999994", types.WhatsAppDomain, 12)
	if err := newTestStoreDevice(primaryStore, adJID, "primary").Save(ctx); err != nil {
		t.Fatalf("save primary device: %v", err)
	}

	manager := NewDeviceManager(primaryStore, nil, nil)
	if err := manager.deleteStoreRowsForJID(ctx, ""); err != nil {
		t.Fatalf("expected empty-jid no-op, got error: %v", err)
	}

	devices, err := primaryStore.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("get all devices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected the unrelated device to be retained, got %d devices", len(devices))
	}
}

// Scenario: the remote-logout callback holds a stale device id. InitWaCLI registers the
// callback with the instance's AD JID string (device.ID.String()), but loadFromRegistry
// can replace that instance with a named registry slot keyed by uuid (same account,
// NonAD JID). keepSlotLogout must then fall back to resolving the slot by JID, so the
// cleanup still deletes the store rows and clears the slot instead of no-opping with
// "device not found" (leaving a stale JID + orphan keys row).
func TestKeepSlotLogout_StaleADJIDFallsBackToJIDResolution(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)
	keysStore := newTestSQLStore(t)

	adJID := types.NewADJID("6281999999996", types.WhatsAppDomain, 14)
	nonAD := adJID.ToNonAD().String()
	if err := newTestStoreDevice(primaryStore, adJID, "primary").Save(ctx); err != nil {
		t.Fatalf("save primary device: %v", err)
	}
	if err := newTestStoreDevice(keysStore, adJID, "keys").Save(ctx); err != nil {
		t.Fatalf("save keys device: %v", err)
	}

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(primaryStore, keysStore, storage)

	// The registry slot that replaced the startup instance: keyed by uuid, NonAD JID.
	const slotID = "slot-uuid-3"
	inst := &DeviceInstance{id: slotID, jid: nonAD, displayName: "tIAtendo", createdAt: time.Now()}
	manager.devices[slotID] = inst

	// The stale id the startup path registered the callback with: the AD JID string.
	if err := manager.keepSlotLogout(ctx, adJID.String()); err != nil {
		t.Fatalf("keepSlotLogout with stale AD JID id returned error: %v", err)
	}

	assertStoreLacksJID(t, ctx, primaryStore, nonAD)
	assertStoreLacksJID(t, ctx, keysStore, nonAD)

	if _, ok := manager.GetDevice(slotID); !ok {
		t.Fatal("expected slot to be kept after stale-id logout")
	}
	if got := inst.JID(); got != "" {
		t.Fatalf("expected instance JID cleared after stale-id logout, got %q", got)
	}
	if len(storage.savedRecords) == 0 {
		t.Fatal("expected SaveDeviceRecord to persist the kept slot")
	}
	last := storage.savedRecords[len(storage.savedRecords)-1]
	if last.DeviceID != slotID || last.JID != "" {
		t.Fatalf("expected persisted slot %s with empty JID, got %s / %q", slotID, last.DeviceID, last.JID)
	}
}

// Scenario: explicit logout for a paired client that is currently DISCONNECTED.
// IsLoggedIn() is only true while connected, so gating the unlink on it silently skips
// the remove-companion-device IQ and the phone keeps showing the linked device forever
// (the local session is deleted, so it can never reconnect to unlink later). The unlink
// must be ATTEMPTED whenever the client is paired (Store.ID set); failure stays
// best-effort and must not fail the local keep-slot cleanup.
func TestLogoutDeviceKeepSlot_AttemptsUnlinkWhenPairedButDisconnected(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)

	adJID := types.NewADJID("6281999999997", types.WhatsAppDomain, 15)
	nonAD := adJID.ToNonAD().String()
	dev := newTestStoreDevice(primaryStore, adJID, "primary")
	if err := dev.Save(ctx); err != nil {
		t.Fatalf("save primary device: %v", err)
	}

	// Real paired client (Store.ID set) that is not connected.
	cli := whatsmeow.NewClient(dev, nil)

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(primaryStore, nil, storage)

	const slotID = "slot-uuid-4"
	inst := &DeviceInstance{id: slotID, jid: nonAD, displayName: "tIAtendo", client: cli, createdAt: time.Now()}
	manager.devices[slotID] = inst

	hook := logrustest.NewLocal(logrus.StandardLogger())
	defer hook.Reset()

	if err := manager.LogoutDeviceKeepSlot(ctx, slotID); err != nil {
		t.Fatalf("LogoutDeviceKeepSlot returned error: %v", err)
	}

	// The unlink attempt fails offline (best-effort), which is observable as the
	// remote-unlink warning. No warning means the attempt was skipped entirely.
	attempted := false
	for _, entry := range hook.AllEntries() {
		if strings.Contains(entry.Message, "remote unlink failed") {
			attempted = true
		}
	}
	if !attempted {
		t.Fatal("expected cli.Logout to be attempted for a paired-but-disconnected client")
	}

	// Local keep-slot cleanup must still have run.
	assertStoreLacksJID(t, ctx, primaryStore, nonAD)
	if _, ok := manager.GetDevice(slotID); !ok {
		t.Fatal("expected slot to be kept after logout")
	}
	if got := inst.JID(); got != "" {
		t.Fatalf("expected instance JID cleared after logout, got %q", got)
	}
}

// Remote logout (events.LoggedOut) keeps the slot: routing the SetOnLoggedOut callback
// through keepSlotLogout (the same behavior both the lazy EnsureClient path and the
// startup InitWaCLI path now use) deletes the whatsmeow rows by JID but preserves the
// slot, instead of deleting it via RemoveDevice (aldinokemal device_manager.go:581).
func TestRemoteLogoutCallback_KeepsSlot(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)
	keysStore := newTestSQLStore(t)

	adJID := types.NewADJID("6281999999995", types.WhatsAppDomain, 13)
	nonAD := adJID.ToNonAD().String()
	if err := newTestStoreDevice(primaryStore, adJID, "primary").Save(ctx); err != nil {
		t.Fatalf("save primary device: %v", err)
	}
	if err := newTestStoreDevice(keysStore, adJID, "keys").Save(ctx); err != nil {
		t.Fatalf("save keys device: %v", err)
	}

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(primaryStore, keysStore, storage)

	inst := &DeviceInstance{id: nonAD, jid: nonAD, displayName: "tIAtendo", createdAt: time.Now()}
	manager.devices[nonAD] = inst

	// Wire the callback exactly as the manager does for remote logout.
	inst.SetOnLoggedOut(func(deviceID string) {
		if err := manager.keepSlotLogout(context.Background(), deviceID); err != nil {
			t.Errorf("keepSlotLogout in remote-logout callback: %v", err)
		}
	})

	inst.TriggerLoggedOut()

	if _, ok := manager.GetDevice(nonAD); !ok {
		t.Fatal("expected slot to be kept on remote logout, but it was removed")
	}
	assertStoreLacksJID(t, ctx, primaryStore, nonAD)
	assertStoreLacksJID(t, ctx, keysStore, nonAD)
}

// A slot id is stored exactly as supplied, padding included. The shared-JID guard must
// therefore compare it verbatim: trimming it would stop " sales " from matching its own
// record, the slot would read as another claim on its own number, and its chat partition
// would survive a purge that reported success.
func TestPurgeDevice_PaddedSlotIDStillDeletesItsOwnJIDPartition(t *testing.T) {
	ctx := context.Background()
	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(nil, nil, storage)

	const slotID = " sales "
	nonAD := "6281700000001@s.whatsapp.net"
	manager.devices[slotID] = &DeviceInstance{id: slotID, jid: nonAD, createdAt: time.Now()}
	storage.deviceRecords = []*domainChatStorage.DeviceRecord{{DeviceID: slotID, JID: nonAD}}

	if err := manager.PurgeDevice(ctx, slotID); err != nil {
		t.Fatalf("PurgeDevice returned error: %v", err)
	}
	if !slices.Contains(storage.deletedData, nonAD) {
		t.Fatalf("expected JID partition %q to be deleted for sole slot %q, got deletes %v", nonAD, slotID, storage.deletedData)
	}
}

// A failed lookup is not evidence that another slot claims the number. Skipping the
// partition is the safe response, but it must surface as an error so the slot stays
// registered and DELETE can be retried rather than reporting a success that lost data.
func TestPurgeDevice_SurfacesSharedJIDLookupFailureAndKeepsSlot(t *testing.T) {
	ctx := context.Background()
	storage := &keepSlotStubStorage{listRecordsErr: errors.New("database is locked")}
	manager := NewDeviceManager(nil, nil, storage)

	const slotID = "solo"
	nonAD := "6281700000002@s.whatsapp.net"
	manager.devices[slotID] = &DeviceInstance{id: slotID, jid: nonAD, createdAt: time.Now()}

	err := manager.PurgeDevice(ctx, slotID)
	if err == nil {
		t.Fatal("expected PurgeDevice to surface the shared-JID lookup failure, got nil")
	}
	if slices.Contains(storage.deletedData, nonAD) {
		t.Fatalf("expected JID partition %q to be skipped when the claim check failed, got deletes %v", nonAD, storage.deletedData)
	}
	if _, ok := manager.GetDevice(slotID); !ok {
		t.Fatal("expected slot to remain registered so the purge can be retried")
	}
}
