package multiuser

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// TestGuardDuplicatePhoneRunsOnARealPairSuccessEvent is the end-to-end proof
// that owners.GuardDuplicatePhone is no longer only unit-tested in isolation:
// Setup wires it into whatsapp.EventHook, the exact package-level hook that
// infrastructure/whatsapp's real event dispatcher (handler(), called from
// whatsmeow's own event callbacks for every live event) invokes for every
// event on every device. This test builds two real, store-backed
// *whatsapp.DeviceInstance values that share one WhatsApp phone number
// (second-companion vs. first), owned by two different WTF users, and fires
// whatsapp.EventHook itself with a genuine *events.PairSuccess — not a call
// into the owners package — exactly as handler() would for a real pairing.
func TestGuardDuplicatePhoneRunsOnARealPairSuccessEvent(t *testing.T) {
	ctx := context.Background()

	primaryStore := newRuntimeTestSQLStore(t)
	phoneJID := types.NewADJID("27825550199", types.WhatsAppDomain, 0)
	secondJID := types.NewADJID("27825550199", types.WhatsAppDomain, 7)

	firstDev := newRuntimeTestStoreDevice(t, primaryStore, phoneJID, "first")
	secondDev := newRuntimeTestStoreDevice(t, primaryStore, secondJID, "second")

	dm := whatsapp.NewDeviceManager(primaryStore, nil, nil)
	instFirst := whatsapp.NewDeviceInstance("d_first", whatsmeow.NewClient(firstDev, nil), nil)
	instSecond := whatsapp.NewDeviceInstance("d_second", whatsmeow.NewClient(secondDev, nil), nil)
	dm.AddDevice(instFirst)
	dm.AddDevice(instSecond)

	s := Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{}}
	rt, err := Setup(s, dm, nil, t.TempDir())
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	t.Cleanup(func() { _ = rt.Close() })

	if whatsapp.EventHook == nil {
		t.Fatal("Setup must install whatsapp.EventHook")
	}

	alice, err := rt.Store.CreateUser(ctx, "+27825550101")
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	bob, err := rt.Store.CreateUser(ctx, "+27825550102")
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	if err := rt.Store.SetOwner(ctx, "d_first", alice.ID); err != nil {
		t.Fatalf("own d_first: %v", err)
	}
	if err := rt.Store.SetOwner(ctx, "d_second", bob.ID); err != nil {
		t.Fatalf("own d_second: %v", err)
	}

	// This is the same call infrastructure/whatsapp's real handler() makes for
	// every live event (see event_handler.go: `if EventHook != nil { runEventHook(...) }`).
	// Firing it directly here — with the package-level var Setup actually set,
	// not a test double — is what proves the wiring is live end to end.
	whatsapp.EventHook(ctx, instSecond, &events.PairSuccess{})

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) && instSecond.JID() != "" {
		time.Sleep(50 * time.Millisecond)
	}

	if got := instSecond.JID(); got != "" {
		t.Fatalf("expected the second (newer) link to be logged out by the duplicate guard, JID still %q", got)
	}
	if got := instFirst.JID(); got == "" {
		t.Fatal("expected the first owner's device to keep its session")
	}
}

func newRuntimeTestSQLStore(t *testing.T) *sqlstore.Container {
	t.Helper()
	uri := sqlite.FormatChatStorageURI("file:"+filepath.Join(t.TempDir(), "whatsmeow.db"), true, true)
	container, err := sqlstore.New(context.Background(), sqlite.DriverName, uri, nil)
	if err != nil {
		t.Fatalf("create sqlstore: %v", err)
	}
	t.Cleanup(func() { _ = container.Close() })
	return container
}

func newRuntimeTestStoreDevice(t *testing.T, container *sqlstore.Container, jid types.JID, pushName string) *store.Device {
	t.Helper()
	device := container.NewDevice()
	device.ID = &jid
	device.PushName = pushName
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{1},
		AccountSignature:    make([]byte, 64),
		AccountSignatureKey: make([]byte, 32),
		DeviceSignature:     make([]byte, 64),
	}
	if err := device.Save(context.Background()); err != nil {
		t.Fatalf("save device %s: %v", jid, err)
	}
	return device
}
