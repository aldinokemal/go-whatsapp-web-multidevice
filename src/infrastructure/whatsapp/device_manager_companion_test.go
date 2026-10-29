package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/types"
)

// Tests for issue #760: when two device slots are linked to the SAME phone number
// (WhatsApp allows several companion devices per account), the slot<->companion
// mapping must be keyed by the full AD JID (number:NN@s.whatsapp.net), never by the
// bare number alone. Guessing among sibling companion rows hijacks or deletes the
// wrong session.

// registryStubStorage extends keepSlotStubStorage with a device registry listing so
// LoadExistingDevices can be exercised with a non-nil storage.
type registryStubStorage struct {
	keepSlotStubStorage
	records []*domainChatStorage.DeviceRecord
}

func (s *registryStubStorage) ListDeviceRecords() ([]*domainChatStorage.DeviceRecord, error) {
	return s.records, nil
}

// countStoreRows returns how many device rows in the container share the NonAD number.
func countStoreRows(t *testing.T, ctx context.Context, m *DeviceManager, nonAD string) int {
	t.Helper()
	devices, err := m.store.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("get all devices: %v", err)
	}
	count := 0
	for _, d := range devices {
		if d != nil && d.ID != nil && d.ID.ToNonAD().String() == nonAD {
			count++
		}
	}
	return count
}

// findStoreDeviceByJID must resolve an AD JID exactly, and must refuse to guess when
// a bare-number lookup matches several sibling companion rows.
func TestFindStoreDeviceByJID_DoesNotGuessAmongSiblingCompanions(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adA := types.NewADJID("6281777000001", types.WhatsAppDomain, 28)
	adB := types.NewADJID("6281777000001", types.WhatsAppDomain, 32)
	if err := newTestStoreDevice(container, adA, "companion-28").Save(ctx); err != nil {
		t.Fatalf("save companion 28: %v", err)
	}
	if err := newTestStoreDevice(container, adB, "companion-32").Save(ctx); err != nil {
		t.Fatalf("save companion 32: %v", err)
	}

	// Exact AD lookup resolves the precise companion.
	dev, err := findStoreDeviceByJID(ctx, container, adB)
	if err != nil {
		t.Fatalf("find by AD JID: %v", err)
	}
	if dev == nil || dev.ID == nil || dev.ID.String() != adB.String() {
		t.Fatalf("expected exact companion %s, got %v", adB.String(), dev)
	}

	// Bare-number lookup with two sibling rows is ambiguous: no guessing.
	dev, err = findStoreDeviceByJID(ctx, container, adA.ToNonAD())
	if err != nil {
		t.Fatalf("ambiguous NonAD lookup should not error, got: %v", err)
	}
	if dev != nil {
		t.Fatalf("expected no device for ambiguous NonAD lookup, got %s", dev.ID.String())
	}
}

// The single-row bare-number fallback must keep working (legacy records that never
// stored an AD JID rely on it).
func TestFindStoreDeviceByJID_SingleRowNonADFallbackStillResolves(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adJID := types.NewADJID("6281777000002", types.WhatsAppDomain, 7)
	if err := newTestStoreDevice(container, adJID, "only").Save(ctx); err != nil {
		t.Fatalf("save device: %v", err)
	}

	dev, err := findStoreDeviceByJID(ctx, container, adJID.ToNonAD())
	if err != nil {
		t.Fatalf("find by NonAD JID: %v", err)
	}
	if dev == nil || dev.ID == nil || dev.ID.String() != adJID.String() {
		t.Fatalf("expected single-row fallback to resolve %s, got %v", adJID.String(), dev)
	}
}

// EnsureClient must attach each slot to its own companion row when both slots know
// their AD JID, instead of handing the first row of the number to both slots.
func TestEnsureClient_ResolvesPreciseCompanionRowByADJID(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adA := types.NewADJID("6281777000003", types.WhatsAppDomain, 28)
	adB := types.NewADJID("6281777000003", types.WhatsAppDomain, 32)
	nonAD := adA.ToNonAD().String()
	if err := newTestStoreDevice(container, adA, "companion-28").Save(ctx); err != nil {
		t.Fatalf("save companion 28: %v", err)
	}
	if err := newTestStoreDevice(container, adB, "companion-32").Save(ctx); err != nil {
		t.Fatalf("save companion 32: %v", err)
	}

	manager := NewDeviceManager(container, nil, nil)
	manager.devices["slot-a"] = &DeviceInstance{id: "slot-a", jid: nonAD, adJID: adA.String(), createdAt: time.Now()}
	manager.devices["slot-b"] = &DeviceInstance{id: "slot-b", jid: nonAD, adJID: adB.String(), createdAt: time.Now()}

	instB, err := manager.EnsureClient(ctx, "slot-b")
	if err != nil {
		t.Fatalf("ensure client slot-b: %v", err)
	}
	clientB := instB.GetClient()
	if clientB == nil || clientB.Store == nil || clientB.Store.ID == nil {
		t.Fatal("expected slot-b to get a paired client")
	}
	if got := clientB.Store.ID.String(); got != adB.String() {
		t.Fatalf("slot-b resolved to %s, want its own companion %s", got, adB.String())
	}

	instA, err := manager.EnsureClient(ctx, "slot-a")
	if err != nil {
		t.Fatalf("ensure client slot-a: %v", err)
	}
	clientA := instA.GetClient()
	if clientA == nil || clientA.Store == nil || clientA.Store.ID == nil {
		t.Fatal("expected slot-a to get a paired client")
	}
	if got := clientA.Store.ID.String(); got != adA.String() {
		t.Fatalf("slot-a resolved to %s, want its own companion %s", got, adA.String())
	}
}

// A slot that only knows its bare number must NOT adopt a companion row that is
// already claimed by a sibling slot's AD JID (session hijack). It should fall back to
// a fresh, unpaired store device so the slot can be re-paired.
func TestEnsureClient_DoesNotHijackSiblingCompanionRow(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adB := types.NewADJID("6281777000004", types.WhatsAppDomain, 32)
	nonAD := adB.ToNonAD().String()
	if err := newTestStoreDevice(container, adB, "companion-32").Save(ctx); err != nil {
		t.Fatalf("save companion 32: %v", err)
	}

	manager := NewDeviceManager(container, nil, nil)
	manager.devices["slot-a"] = &DeviceInstance{id: "slot-a", jid: nonAD, createdAt: time.Now()}
	manager.devices["slot-b"] = &DeviceInstance{id: "slot-b", jid: nonAD, adJID: adB.String(), createdAt: time.Now()}

	instA, err := manager.EnsureClient(ctx, "slot-a")
	if err != nil {
		t.Fatalf("ensure client slot-a: %v", err)
	}
	clientA := instA.GetClient()
	if clientA == nil || clientA.Store == nil {
		t.Fatal("expected slot-a to get a client")
	}
	if clientA.Store.ID != nil && clientA.Store.ID.String() == adB.String() {
		t.Fatalf("slot-a hijacked sibling companion row %s", adB.String())
	}
	if clientA.Store.ID != nil {
		t.Fatalf("expected slot-a to get a fresh unpaired device, got %s", clientA.Store.ID.String())
	}
}

// deleteStoreRowsForJID must refuse an ambiguous bare-number delete when several
// sibling companion rows exist (it cannot know which one belongs to the slot).
func TestDeleteStoreRowsForJID_RefusesAmbiguousBareNumber(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adA := types.NewADJID("6281777000005", types.WhatsAppDomain, 28)
	adB := types.NewADJID("6281777000005", types.WhatsAppDomain, 32)
	nonAD := adA.ToNonAD().String()
	if err := newTestStoreDevice(container, adA, "companion-28").Save(ctx); err != nil {
		t.Fatalf("save companion 28: %v", err)
	}
	if err := newTestStoreDevice(container, adB, "companion-32").Save(ctx); err != nil {
		t.Fatalf("save companion 32: %v", err)
	}

	manager := NewDeviceManager(container, nil, nil)
	if err := manager.deleteStoreRowsForJID(ctx, nonAD); err != nil {
		t.Fatalf("ambiguous delete should be a warn-and-skip, got error: %v", err)
	}

	if got := countStoreRows(t, ctx, manager, nonAD); got != 2 {
		t.Fatalf("expected both sibling rows to survive an ambiguous delete, got %d", got)
	}
}

// deleteStoreRowsForJID with a full AD JID must delete exactly that companion row.
func TestDeleteStoreRowsForJID_DeletesExactCompanionOnly(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adA := types.NewADJID("6281777000006", types.WhatsAppDomain, 28)
	adB := types.NewADJID("6281777000006", types.WhatsAppDomain, 32)
	if err := newTestStoreDevice(container, adA, "companion-28").Save(ctx); err != nil {
		t.Fatalf("save companion 28: %v", err)
	}
	if err := newTestStoreDevice(container, adB, "companion-32").Save(ctx); err != nil {
		t.Fatalf("save companion 32: %v", err)
	}

	manager := NewDeviceManager(container, nil, nil)
	if err := manager.deleteStoreRowsForJID(ctx, adA.String()); err != nil {
		t.Fatalf("delete by AD JID: %v", err)
	}

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("get all devices: %v", err)
	}
	if len(devices) != 1 || devices[0].ID.String() != adB.String() {
		t.Fatalf("expected only sibling %s to survive, got %d rows", adB.String(), len(devices))
	}
}

// Deleting by a full AD JID must never take a bare-number (Device-0) row of the same
// number with it: such a row may be a legacy slot's usable session, and only the exact
// companion row certainly belongs to the target.
func TestDeleteStoreRowsForJID_ADTargetLeavesBareNumberRowAlone(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adJID := types.NewADJID("6281777000015", types.WhatsAppDomain, 28)
	nonADJID := adJID.ToNonAD()
	if err := newTestStoreDevice(container, adJID, "companion-28").Save(ctx); err != nil {
		t.Fatalf("save companion 28: %v", err)
	}
	if err := newTestStoreDevice(container, nonADJID, "bare-number").Save(ctx); err != nil {
		t.Fatalf("save bare-number row: %v", err)
	}

	manager := NewDeviceManager(container, nil, nil)
	if err := manager.deleteStoreRowsForJID(ctx, adJID.String()); err != nil {
		t.Fatalf("delete by AD JID: %v", err)
	}

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("get all devices: %v", err)
	}
	if len(devices) != 1 || devices[0].ID.String() != nonADJID.String() {
		t.Fatalf("expected only the bare-number row to survive, got %d rows", len(devices))
	}
}

// keepSlotLogout must use the slot's AD JID so it deletes only its own companion row,
// leaving the sibling slot's session untouched.
func TestKeepSlotLogout_DeletesOnlyOwnCompanionRow(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adA := types.NewADJID("6281777000007", types.WhatsAppDomain, 28)
	adB := types.NewADJID("6281777000007", types.WhatsAppDomain, 32)
	nonAD := adA.ToNonAD().String()
	if err := newTestStoreDevice(container, adA, "companion-28").Save(ctx); err != nil {
		t.Fatalf("save companion 28: %v", err)
	}
	if err := newTestStoreDevice(container, adB, "companion-32").Save(ctx); err != nil {
		t.Fatalf("save companion 32: %v", err)
	}

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(container, nil, storage)
	instA := &DeviceInstance{id: "slot-a", jid: nonAD, adJID: adA.String(), createdAt: time.Now()}
	manager.devices["slot-a"] = instA
	manager.devices["slot-b"] = &DeviceInstance{id: "slot-b", jid: nonAD, adJID: adB.String(), createdAt: time.Now()}

	if err := manager.keepSlotLogout(ctx, "slot-a"); err != nil {
		t.Fatalf("keepSlotLogout: %v", err)
	}

	devices, err := container.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("get all devices: %v", err)
	}
	if len(devices) != 1 || devices[0].ID.String() != adB.String() {
		t.Fatalf("expected sibling companion %s to survive, got %d rows", adB.String(), len(devices))
	}
	if got := instA.ADJID(); got != "" {
		t.Fatalf("expected slot-a AD JID cleared after logout, got %q", got)
	}
	if len(storage.savedRecords) == 0 {
		t.Fatal("expected the kept slot to be persisted")
	}
	last := storage.savedRecords[len(storage.savedRecords)-1]
	if last.DeviceID != "slot-a" || last.JID != "" || last.ADJID != "" {
		t.Fatalf("expected persisted slot-a with cleared JIDs, got %+v", last)
	}
}

// loadFromRegistry must keep two slots that share a phone number when their AD JIDs
// identify distinct companions — deleting one is data loss.
func TestLoadFromRegistry_KeepsSiblingSlotsOnSameNumber(t *testing.T) {
	adA := types.NewADJID("6281777000008", types.WhatsAppDomain, 28)
	adB := types.NewADJID("6281777000008", types.WhatsAppDomain, 32)
	nonAD := adA.ToNonAD().String()

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(nil, nil, storage)

	manager.loadFromRegistry([]*domainChatStorage.DeviceRecord{
		{DeviceID: "slot-a", DisplayName: "A", JID: nonAD, ADJID: adA.String(), CreatedAt: time.Now()},
		{DeviceID: "slot-b", DisplayName: "B", JID: nonAD, ADJID: adB.String(), CreatedAt: time.Now()},
	})

	if len(storage.deletedRecords) != 0 {
		t.Fatalf("expected no registry deletions, got %v", storage.deletedRecords)
	}
	instA, okA := manager.GetDevice("slot-a")
	instB, okB := manager.GetDevice("slot-b")
	if !okA || !okB {
		t.Fatalf("expected both sibling slots to load, got a=%v b=%v", okA, okB)
	}
	if instA.ADJID() != adA.String() || instB.ADJID() != adB.String() {
		t.Fatalf("expected AD JIDs to load, got a=%q b=%q", instA.ADJID(), instB.ADJID())
	}
}

// Legacy duplicate records (same bare number, no AD JID) are ambiguous: the loader
// must skip the duplicate but MUST NOT delete its registry record at boot.
func TestLoadFromRegistry_LegacyDuplicateSkippedNotDeleted(t *testing.T) {
	nonAD := "6281777000009@s.whatsapp.net"

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(nil, nil, storage)

	manager.loadFromRegistry([]*domainChatStorage.DeviceRecord{
		{DeviceID: "slot-a", JID: nonAD, CreatedAt: time.Now()},
		{DeviceID: "slot-b", JID: nonAD, CreatedAt: time.Now()},
	})

	if len(storage.deletedRecords) != 0 {
		t.Fatalf("boot reconciliation must not delete registry records, got %v", storage.deletedRecords)
	}
	if _, ok := manager.GetDevice("slot-a"); !ok {
		t.Fatal("expected first slot to load")
	}
	if _, ok := manager.GetDevice("slot-b"); ok {
		t.Fatal("expected ambiguous duplicate slot to be skipped in memory")
	}
}

// The auto-created-duplicate cleanup must also become skip-and-warn instead of
// deleting records during startup reconciliation.
func TestLoadFromRegistry_AutoCreatedDuplicateSkippedNotDeleted(t *testing.T) {
	nonAD := "6281777000010@s.whatsapp.net"

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(nil, nil, storage)

	manager.loadFromRegistry([]*domainChatStorage.DeviceRecord{
		{DeviceID: "slot-a", JID: nonAD, CreatedAt: time.Now()},
		{DeviceID: nonAD, JID: nonAD, CreatedAt: time.Now()}, // auto-created duplicate
	})

	if len(storage.deletedRecords) != 0 {
		t.Fatalf("boot reconciliation must not delete registry records, got %v", storage.deletedRecords)
	}
	if _, ok := manager.GetDevice("slot-a"); !ok {
		t.Fatal("expected manual slot to load")
	}
	if _, ok := manager.GetDevice(nonAD); ok {
		t.Fatal("expected auto-created duplicate to be skipped in memory")
	}
}

// LoadExistingDevices must backfill the AD JID for a legacy slot that only stored the
// bare number, and persist the enriched record.
func TestLoadExistingDevices_BackfillsADJIDForLegacySlot(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adJID := types.NewADJID("6281777000011", types.WhatsAppDomain, 28)
	nonAD := adJID.ToNonAD().String()
	if err := newTestStoreDevice(container, adJID, "companion-28").Save(ctx); err != nil {
		t.Fatalf("save companion 28: %v", err)
	}

	storage := &registryStubStorage{}
	manager := NewDeviceManager(container, nil, storage)
	inst := &DeviceInstance{id: "slot-a", jid: nonAD, displayName: "A", createdAt: time.Now()}
	manager.devices["slot-a"] = inst

	if err := manager.LoadExistingDevices(ctx); err != nil {
		t.Fatalf("load existing devices: %v", err)
	}

	if got := inst.ADJID(); got != adJID.String() {
		t.Fatalf("expected backfilled AD JID %s, got %q", adJID.String(), got)
	}
	found := false
	for _, rec := range storage.savedRecords {
		if rec.DeviceID == "slot-a" && rec.ADJID == adJID.String() && rec.JID == nonAD {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected backfilled record persisted, got %+v", storage.savedRecords)
	}
}

// LoadExistingDevices must not adopt sibling companion rows of a number that is
// already claimed by a slot — those orphans are left for explicit cleanup, so the
// auto-connect loop cannot dial dead sessions of a live number.
func TestLoadExistingDevices_DoesNotAdoptSiblingCompanionRows(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adA := types.NewADJID("6281777000012", types.WhatsAppDomain, 28)
	adB := types.NewADJID("6281777000012", types.WhatsAppDomain, 32)
	nonAD := adA.ToNonAD().String()
	if err := newTestStoreDevice(container, adA, "companion-28").Save(ctx); err != nil {
		t.Fatalf("save companion 28: %v", err)
	}
	if err := newTestStoreDevice(container, adB, "companion-32").Save(ctx); err != nil {
		t.Fatalf("save companion 32: %v", err)
	}

	storage := &registryStubStorage{}
	manager := NewDeviceManager(container, nil, storage)
	inst := &DeviceInstance{id: "slot-a", jid: nonAD, adJID: adA.String(), createdAt: time.Now()}
	manager.devices["slot-a"] = inst

	if err := manager.LoadExistingDevices(ctx); err != nil {
		t.Fatalf("load existing devices: %v", err)
	}

	if got := len(manager.ListDevices()); got != 1 {
		t.Fatalf("expected sibling orphan row to stay unadopted, got %d instances", got)
	}
	if got := inst.ADJID(); got != adA.String() {
		t.Fatalf("expected slot-a to keep its own companion, got %q", got)
	}
}

// Adopting a store row for a completely unclaimed number must record the precise AD
// JID on the adopted instance (the legacy single-to-multi-device migration path).
func TestLoadExistingDevices_AdoptsUnclaimedRowWithPreciseADJID(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)

	adJID := types.NewADJID("6281777000013", types.WhatsAppDomain, 5)
	nonAD := adJID.ToNonAD().String()
	if err := newTestStoreDevice(container, adJID, "companion-5").Save(ctx); err != nil {
		t.Fatalf("save companion 5: %v", err)
	}

	manager := NewDeviceManager(container, nil, nil)
	if err := manager.LoadExistingDevices(ctx); err != nil {
		t.Fatalf("load existing devices: %v", err)
	}

	inst, ok := manager.GetDevice(nonAD)
	if !ok {
		t.Fatalf("expected adopted instance for %s", nonAD)
	}
	if inst.JID() != nonAD {
		t.Fatalf("expected adopted instance JID %s, got %q", nonAD, inst.JID())
	}
	if inst.ADJID() != adJID.String() {
		t.Fatalf("expected adopted instance AD JID %s, got %q", adJID.String(), inst.ADJID())
	}
}

// syncKeysDevice must distinguish sibling companions: a keys row for companion :32
// does not satisfy companion :28, whose keys row must still be synced.
func TestSyncKeysDevice_DistinguishesSiblingCompanions(t *testing.T) {
	ctx := context.Background()
	primaryStore := newTestSQLStore(t)
	keysStore := newTestSQLStore(t)

	adA := types.NewADJID("6281777000014", types.WhatsAppDomain, 28)
	adB := types.NewADJID("6281777000014", types.WhatsAppDomain, 32)
	if err := newTestStoreDevice(primaryStore, adA, "primary-28").Save(ctx); err != nil {
		t.Fatalf("save primary 28: %v", err)
	}
	if err := newTestStoreDevice(primaryStore, adB, "primary-32").Save(ctx); err != nil {
		t.Fatalf("save primary 32: %v", err)
	}
	if err := newTestStoreDevice(keysStore, adB, "keys-32").Save(ctx); err != nil {
		t.Fatalf("save keys 32: %v", err)
	}

	syncKeysDevice(ctx, primaryStore, keysStore, adA)

	devices, err := keysStore.GetAllDevices(ctx)
	if err != nil {
		t.Fatalf("get keys devices: %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("expected keys DB to hold both sibling companions, got %d", len(devices))
	}
	assertStoreHasDevice(t, devices, adA.String())
	assertStoreHasDevice(t, devices, adB.String())
}
