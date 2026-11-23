package multiuser

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Gap A, end to end through the real event hook: a connected slot's number is
// remembered, and a sign-out from the phone keeps it and marks the slot.
func TestASlotKeepsItsNumberWhenThePhoneSignsItOut(t *testing.T) {
	ctx := context.Background()
	container := newRuntimeTestSQLStore(t)
	dev := newRuntimeTestStoreDevice(t, container, types.NewADJID("27825550177", types.WhatsAppDomain, 3), "Alice")
	dm := whatsapp.NewDeviceManager(container, nil, nil)
	inst := whatsapp.NewDeviceInstance("d_slot", whatsmeow.NewClient(dev, nil), nil)
	dm.AddDevice(inst)

	s := Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{}}
	rt, err := Setup(s, dm, nil, t.TempDir())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	alice, _ := rt.Store.CreateUser(ctx, "+27825550101")
	if err := rt.Store.SetOwner(ctx, "d_slot", alice.ID); err != nil {
		t.Fatal(err)
	}

	whatsapp.EventHook(ctx, inst, &events.Connected{})
	slots, _ := rt.Store.SlotsOf(ctx, alice.ID)
	if got := slots["d_slot"]; got.LastPhone != "27825550177" || got.LastName != "Alice" || got.SignedOutAt != nil {
		t.Fatalf("after Connected: %+v", got)
	}

	whatsapp.EventHook(ctx, inst, &events.LoggedOut{})
	slots, _ = rt.Store.SlotsOf(ctx, alice.ID)
	got := slots["d_slot"]
	if got.LastPhone != "27825550177" || got.SignedOutAt == nil {
		t.Fatalf("after LoggedOut: %+v", got)
	}
}

// Gap B: an accepted link clears an earlier refusal.
func TestAnAcceptedLinkClearsAnEarlierRefusal(t *testing.T) {
	ctx := context.Background()
	container := newRuntimeTestSQLStore(t)
	dev := newRuntimeTestStoreDevice(t, container, types.NewADJID("27825550178", types.WhatsAppDomain, 4), "Bob")
	dm := whatsapp.NewDeviceManager(container, nil, nil)
	inst := whatsapp.NewDeviceInstance("d_ok", whatsmeow.NewClient(dev, nil), nil)
	dm.AddDevice(inst)
	s := Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{}}
	rt, err := Setup(s, dm, nil, t.TempDir())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	bob, _ := rt.Store.CreateUser(ctx, "+27825550102")
	_ = rt.Store.SetOwner(ctx, "d_ok", bob.ID)
	_ = rt.Store.RecordRefusal(ctx, "d_ok", "duplicate")

	whatsapp.EventHook(ctx, inst, &events.PairSuccess{})
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		slots, _ := rt.Store.SlotsOf(ctx, bob.ID)
		if slots["d_ok"].Refusal == "" {
			if slots["d_ok"].LastPhone != "27825550178" {
				t.Fatalf("identity not recorded: %+v", slots["d_ok"])
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the refusal was not cleared after an accepted link")
}

func TestSetupHardensWebhookDeliveryAndCloseReleasesIt(t *testing.T) {
	s := Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{}}
	rt, err := Setup(s, whatsapp.NewDeviceManager(nil, nil, nil), nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if whatsapp.WebhookClientHook == nil {
		t.Fatal("Setup must install the webhook client hook")
	}
	_ = rt.Close()
	if whatsapp.WebhookClientHook != nil {
		t.Fatal("Close must release the webhook client hook")
	}
}
