// src/infrastructure/whatsapp/device_manager_current_instance_test.go
package whatsapp

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/types"
)

// This is the real bug a live sign-in report traced all the way down to: the
// remote-logout callback's own fix (keepSlotLogout's comment, same file) already
// documents the mechanism, but the general event-dispatch path never got the
// same treatment. InitWaCLI (and EnsureClient's reconnect path) register their
// event-handler closure once, at client creation, over whichever *DeviceInstance
// existed then - keyed by the client's own AD JID string. loadFromRegistry's
// one-time boot reconciliation can later move that same live client onto a
// brand-new instance keyed by the registry's canonical device id instead - and
// nothing ever told the already-registered closure, so every event for that
// device reported the stale AD-JID-shaped id forever, never the canonical one
// every other part of the system (settings, admin tools, ServiceDeviceID) uses.
func TestCurrentInstanceFor_ResolvesPastLoadFromRegistrysMigration(t *testing.T) {
	adJID := types.NewADJID("27722983912", types.WhatsAppDomain, 59)
	nonAD := adJID.ToNonAD().String()

	// The startup instance, keyed exactly as InitWaCLI keys it: instanceID :=
	// device.ID.String() (the AD JID string), not a registry device id.
	startupInstance := &DeviceInstance{id: adJID.String(), jid: nonAD, adJID: adJID.String(), createdAt: time.Now()}

	storage := &keepSlotStubStorage{}
	manager := NewDeviceManager(nil, nil, storage)
	manager.AddDevice(startupInstance)

	// The real migration path: the registry (multi-user) layer loads its own
	// canonical-id record for the same companion session.
	manager.loadFromRegistry([]*domainChatStorage.DeviceRecord{
		{DeviceID: "d_canonical", DisplayName: "Aryeh Lewis", JID: nonAD, ADJID: adJID.String(), CreatedAt: time.Now()},
	})

	canonical, ok := manager.GetDevice("d_canonical")
	if !ok {
		t.Fatal("expected the registry migration to register the canonical device")
	}
	if _, stillThere := manager.GetDevice(adJID.String()); stillThere {
		t.Fatal("expected the startup slot to be replaced, not kept alongside the canonical one")
	}

	// The whole point: a closure still holding the stale startupInstance must
	// resolve to the canonical instance that actually replaced it.
	current := manager.CurrentInstanceFor(startupInstance)
	if current != canonical {
		t.Fatalf("expected CurrentInstanceFor to resolve to the canonical instance %q, got %q", canonical.ID(), current.ID())
	}
}

// The common case, with no migration ever having happened: an instance still
// registered under its own id resolves to itself.
func TestCurrentInstanceFor_StillCurrentReturnsItself(t *testing.T) {
	manager := NewDeviceManager(nil, nil, nil)
	inst := &DeviceInstance{id: "d_only", createdAt: time.Now()}
	manager.AddDevice(inst)

	if got := manager.CurrentInstanceFor(inst); got != inst {
		t.Fatalf("expected the same, still-registered instance back, got %v", got)
	}
}

// An instance the manager has never heard of (e.g. a single-device deployment
// with no multi-user registry at all) falls back to itself rather than nil.
func TestCurrentInstanceFor_UnknownInstanceFallsBackToItself(t *testing.T) {
	manager := NewDeviceManager(nil, nil, nil)
	inst := &DeviceInstance{id: "d_never_registered", createdAt: time.Now()}

	if got := manager.CurrentInstanceFor(inst); got != inst {
		t.Fatalf("expected the original instance back when the manager has no record of it, got %v", got)
	}
}

func TestCurrentInstanceFor_NilSafety(t *testing.T) {
	var nilManager *DeviceManager
	if got := nilManager.CurrentInstanceFor(&DeviceInstance{id: "x"}); got == nil {
		t.Fatal("expected the fallback instance back from a nil manager, not nil")
	}

	manager := NewDeviceManager(nil, nil, nil)
	if got := manager.CurrentInstanceFor(nil); got != nil {
		t.Fatalf("expected nil back for a nil fallback instance, got %v", got)
	}
}
