package whatsapp

import (
	"context"
	"errors"
	"net"
	"net/http"
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

// The hook runs before upstream handling, and a panicking hook never stops it.
func TestEventHookRunsFirstAndPanicDoesNotStopUpstream(t *testing.T) {
	useNoopLog(t)
	saved := EventHook
	t.Cleanup(func() { EventHook = saved })

	var order []string
	base := &deleteForMeRepo{order: &order}
	inst := NewDeviceInstance("d_slot", nil, newDeviceChatStorage("d_slot", base))

	EventHook = func(context.Context, *DeviceInstance, any) { order = append(order, "hook") }
	handler(context.Background(), inst, deleteForMeEvent())
	if len(order) != 2 || order[0] != "hook" || order[1] != "upstream" {
		t.Fatalf("expected hook then upstream, got %v", order)
	}

	order = nil
	EventHook = func(context.Context, *DeviceInstance, any) {
		order = append(order, "hook")
		panic("boom")
	}
	handler(context.Background(), inst, deleteForMeEvent())
	if len(order) != 2 || order[1] != "upstream" {
		t.Fatalf("a panicking hook must not stop upstream handling, got %v", order)
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
	found          *domainChatStorage.Message
	deletedDevice  string
	order          *[]string
}

func (r *deleteForMeRepo) DeleteMessageByDevice(deviceID, id, chatJID string) error {
	r.deletedDevice = deviceID
	return nil
}

func (r *deleteForMeRepo) GetMessageByID(string) (*domainChatStorage.Message, error) {
	r.unscopedCalled = true
	return &domainChatStorage.Message{ID: "M", ChatJID: "x@s.whatsapp.net", DeviceID: "someone-else"}, nil
}

func (r *deleteForMeRepo) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	r.scopedDevice = deviceID
	if r.order != nil {
		*r.order = append(*r.order, "upstream")
	}
	return r.found, nil
}

func useNoopLog(t *testing.T) {
	t.Helper()
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })
}

func deleteForMeEvent() *events.DeleteForMe {
	evt := &events.DeleteForMe{}
	evt.MessageID = "M"
	return evt
}

// Until restart the wrapper is keyed by the slot id ("d_..."), while messages are
// written under the instance's JID. The lookup must use the JID.
func TestDeleteForMeUsesInstanceJIDNotWrapperSlotID(t *testing.T) {
	useNoopLog(t)
	base := &deleteForMeRepo{}
	inst := NewDeviceInstance("d_slot", nil, newDeviceChatStorage("d_slot", base))
	inst.jid = "6281777000098@s.whatsapp.net"

	handler(context.Background(), inst, deleteForMeEvent())

	if base.unscopedCalled {
		t.Fatal("unscoped GetMessageByID was used")
	}
	if base.scopedDevice != "6281777000098@s.whatsapp.net" {
		t.Fatalf("expected lookup under the instance JID, got %q", base.scopedDevice)
	}
}

// An unpaired instance (no JID) falls back to its slot id, like the writers do.
func TestDeleteForMeFallsBackToSlotIDWithoutJID(t *testing.T) {
	useNoopLog(t)
	base := &deleteForMeRepo{}
	inst := NewDeviceInstance("d_slot", nil, newDeviceChatStorage("d_other", base))

	handler(context.Background(), inst, deleteForMeEvent())

	if base.scopedDevice != "d_slot" {
		t.Fatalf("expected lookup under the slot id, got %q", base.scopedDevice)
	}
}

// The row found is deleted under its own device id, not the wrapper's.
func TestDeleteForMeDeletesUnderTheFoundRowsDevice(t *testing.T) {
	useNoopLog(t)
	jid := "6281777000097@s.whatsapp.net"
	base := &deleteForMeRepo{found: &domainChatStorage.Message{ID: "M", ChatJID: "x@s.whatsapp.net", DeviceID: jid}}
	inst := NewDeviceInstance("d_slot", nil, newDeviceChatStorage("d_slot", base))
	inst.jid = jid

	handler(context.Background(), inst, deleteForMeEvent())

	if base.deletedDevice != jid {
		t.Fatalf("expected delete under %q, got %q", jid, base.deletedDevice)
	}
}

// "Delete for me" on one account must never find (and delete) another account's
// copy of a message with the same id.
func TestDeleteForMeLooksUpOnlyThisDevicesMessage(t *testing.T) {
	useNoopLog(t)
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

	const phoneJID = "6281777000021@s.whatsapp.net"
	var manager *DeviceManager
	run := func(disable bool) (*DeviceInstance, *registryStubStorage) {
		DisableOrphanSlotAdoption = disable
		ctx := context.Background()
		container := newTestSQLStore(t)
		adJID := types.NewADJID("6281777000021", types.WhatsAppDomain, 9)
		if err := newTestStoreDevice(container, adJID, "companion-9").Save(ctx); err != nil {
			t.Fatalf("save companion: %v", err)
		}
		storage := &registryStubStorage{}
		manager = NewDeviceManager(container, nil, storage)
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
	// The session lands in its own new instance keyed by the phone JID (upstream's
	// "no orphan slot" path), which no user owns.
	own, ok := manager.GetDevice(phoneJID)
	if !ok {
		t.Fatalf("expected the session in its own instance %s", phoneJID)
	}
	if own == other || own.JID() != phoneJID || own.ADJID() != "6281777000021:9@s.whatsapp.net" {
		t.Fatalf("unexpected own instance: id=%q jid=%q ad=%q", own.ID(), own.JID(), own.ADJID())
	}
	if got := len(manager.ListDevices()); got != 2 {
		t.Fatalf("expected the unpaired slot plus the session's own instance, got %d", got)
	}
}

// The webhook client hook sees the transport and client of every delivery, so
// multi-user mode can refuse what a shared server must not reach.
func TestWebhookClientHookCanRefuseADelivery(t *testing.T) {
	saved := WebhookClientHook
	t.Cleanup(func() { WebhookClientHook = saved })
	var called bool
	WebhookClientHook = func(tr *http.Transport, _ *http.Client) {
		called = true
		tr.DialContext = func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("refused by hook")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := submitWebhook(ctx, map[string]any{"a": 1}, "http://127.0.0.1:9/hook", nil)
	if !called || err == nil {
		t.Fatalf("hook called=%v err=%v", called, err)
	}
}
