package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type wtfUnknownEvent struct{}

func TestEventHookSeesEveryEventWithDeviceContext(t *testing.T) {
	saved := EventHook
	t.Cleanup(func() { EventHook = saved })

	inst := NewDeviceInstance("d_hook", nil, nil)
	var gotInst *DeviceInstance
	var gotEvt any
	var ctxInst *DeviceInstance
	EventHook = func(ctx context.Context, instance *DeviceInstance, evt any) {
		gotInst, gotEvt = instance, evt
		ctxInst, _ = DeviceFromContext(ctx)
	}

	evt := &wtfUnknownEvent{}
	handler(context.Background(), inst, evt)

	if gotInst != inst || gotEvt != evt {
		t.Fatalf("hook not called with the instance and event: %v %v", gotInst, gotEvt)
	}
	if ctxInst != inst {
		t.Fatalf("hook context must carry the device, got %v", ctxInst)
	}
}

func TestEventHookNilIsNoOp(t *testing.T) {
	saved := EventHook
	t.Cleanup(func() { EventHook = saved })
	EventHook = nil
	handler(context.Background(), NewDeviceInstance("d_hook", nil, nil), &wtfUnknownEvent{})
}

type deleteForMeRepo struct {
	domainChatStorage.IChatStorageRepository
	unscopedCalled bool
	scopedDevice   string
}

func (r *deleteForMeRepo) GetMessageByID(string) (*domainChatStorage.Message, error) {
	r.unscopedCalled = true
	return &domainChatStorage.Message{ID: "M", ChatJID: "x@s.whatsapp.net", DeviceID: "someone-else"}, nil
}

func (r *deleteForMeRepo) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	r.scopedDevice = deviceID
	return nil, nil
}

// "Delete for me" on one account must never find (and delete) another account's
// copy of a message with the same id.
func TestDeleteForMeLooksUpOnlyThisDevicesMessage(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })

	base := &deleteForMeRepo{}
	repo := newDeviceChatStorage("6281777000099@s.whatsapp.net", base)
	evt := &events.DeleteForMe{}
	evt.MessageID = "M"

	handleDeleteForMe(context.Background(), evt, repo, "6281777000099@s.whatsapp.net", nil)

	if base.unscopedCalled {
		t.Fatal("unscoped GetMessageByID was used")
	}
	if base.scopedDevice != "6281777000099@s.whatsapp.net" {
		t.Fatalf("expected lookup scoped to the wrapper's device, got %q", base.scopedDevice)
	}
}

// With DisableOrphanSlotAdoption set (multi-user mode), a store session whose slot
// record was lost must not be attached to some other unpaired slot.
func TestLoadExistingDevices_OrphanAdoptionCanBeDisabled(t *testing.T) {
	saved := DisableOrphanSlotAdoption
	t.Cleanup(func() { DisableOrphanSlotAdoption = saved })

	run := func(disable bool) (*DeviceInstance, *registryStubStorage) {
		DisableOrphanSlotAdoption = disable
		ctx := context.Background()
		container := newTestSQLStore(t)
		adJID := types.NewADJID("6281777000021", types.WhatsAppDomain, 9)
		if err := newTestStoreDevice(container, adJID, "companion-9").Save(ctx); err != nil {
			t.Fatalf("save companion: %v", err)
		}
		storage := &registryStubStorage{}
		manager := NewDeviceManager(container, nil, storage)
		other := &DeviceInstance{id: "d_other_users_slot", createdAt: time.Now()}
		manager.devices[other.id] = other
		if err := manager.LoadExistingDevices(ctx); err != nil {
			t.Fatalf("load existing devices: %v", err)
		}
		return other, storage
	}

	// Upstream behaviour (default): the unpaired slot adopts the session.
	other, _ := run(false)
	if other.JID() == "" {
		t.Fatal("expected upstream orphan adoption with the flag off")
	}

	other, storage := run(true)
	if other.JID() != "" || other.ADJID() != "" {
		t.Fatalf("unpaired slot adopted another session: jid=%q ad=%q", other.JID(), other.ADJID())
	}
	for _, rec := range storage.savedRecords {
		if rec.DeviceID == "d_other_users_slot" {
			t.Fatalf("adoption was persisted: %+v", rec)
		}
	}
}
