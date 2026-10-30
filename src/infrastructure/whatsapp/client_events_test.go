package whatsapp

import (
	"context"
	"go.mau.fi/whatsmeow"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestStartupEventsFollowRegistryOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	container := newTestSQLStore(t)
	keys := newTestSQLStore(t)
	ad := types.NewADJID("6281777000099", types.WhatsAppDomain, 28)
	require.NoError(t, newTestStoreDevice(container, ad, "Sales").Save(ctx))
	storage := &registryStubStorage{records: []*domainChatStorage.DeviceRecord{
		{DeviceID: "slot-a", DisplayName: "Sales", JID: ad.ToNonAD().String(), ADJID: ad.String()},
	}}
	globalStateMu.Lock()
	oldManager, oldClient, oldDB, oldKeys := deviceManager, cli, db, keysDB
	deviceManager = nil
	globalStateMu.Unlock()
	oldOS, oldPlatform := store.DeviceProps.Os, store.DeviceProps.PlatformType
	oldChatwoot := config.ChatwootEnabled
	config.ChatwootEnabled = false
	oldBroadcast := websocket.Broadcast
	websocket.Broadcast = make(chan websocket.BroadcastMessage, 10)
	t.Cleanup(func() {
		websocket.Broadcast = oldBroadcast
		config.ChatwootEnabled = oldChatwoot
		store.DeviceProps.Os, store.DeviceProps.PlatformType = oldOS, oldPlatform
		globalStateMu.Lock()
		deviceManager, cli, db, keysDB = oldManager, oldClient, oldDB, oldKeys
		globalStateMu.Unlock()
	})
	got := captureDeviceWebhooks(t)
	client := InitWaCLI(ctx, container, keys, storage)
	manager := GetDeviceManager()
	startup, ok := manager.GetDevice(ad.String())
	require.True(t, ok)
	dispatch := manager.eventHandler(client)
	cancel()
	require.NoError(t, manager.LoadExistingDevices(context.Background()))
	current, ok := manager.GetDevice("slot-a")
	require.True(t, ok)
	require.NotSame(t, startup, current)
	require.Same(t, client, current.GetClient())
	keyDevice, err := keys.GetDevice(context.Background(), ad)
	require.NoError(t, err)
	require.NotNil(t, keyDevice)
	require.NoError(t, keys.DeleteDevice(context.Background(), keyDevice))
	dispatch(&events.PairSuccess{ID: ad})
	require.Equal(t, "LOGIN_SUCCESS", (<-websocket.Broadcast).Code)
	paired := recvWebhook(t, got).body
	require.Equal(t, "connection", paired["event"])
	require.Equal(t, "slot-a", paired["session_id"])
	keyDevice, err = keys.GetDevice(context.Background(), ad)
	require.NoError(t, err)
	require.NotNil(t, keyDevice, "pairing must sync keys even after the startup context is cancelled")
	require.NoError(t, newTestStoreDevice(container, types.NewADJID(ad.User, types.WhatsAppDomain, 32), "sibling").Save(context.Background()))
	dispatch(&events.TemporaryBan{})
	require.Equal(t, "slot-a", recvWebhook(t, got).body["session_id"])
	require.Equal(t, "slot-a", (<-websocket.Broadcast).Result.(map[string]any)["device_id"])
	dispatch(&events.LoggedOut{})
	require.Equal(t, "slot-a", recvWebhook(t, got).body["session_id"])
	require.Nil(t, current.GetClient())
	require.Empty(t, current.JID())
	require.Empty(t, current.ADJID())
	require.Equal(t, "Sales", current.DisplayName())
	require.Empty(t, storage.deletedData)
	require.Empty(t, storage.deletedRecords)
	require.Equal(t, "slot-a", storage.savedRecords[len(storage.savedRecords)-1].DeviceID)
	require.Empty(t, storage.savedRecords[len(storage.savedRecords)-1].ADJID)
	require.Equal(t, 1, countStoreRows(t, context.Background(), manager, ad.ToNonAD().String()))
	require.Empty(t, got)
	require.Nil(t, manager.eventOwner(client))
}

func eventTestBroadcast(t *testing.T) {
	t.Helper()
	old := websocket.Broadcast
	websocket.Broadcast = make(chan websocket.BroadcastMessage, 20)
	t.Cleanup(func() { websocket.Broadcast = old })
}

func TestClientEventsSelectExactOwnerAndRejectObsoleteOrAmbiguousClient(t *testing.T) {
	eventTestBroadcast(t)
	container := newTestSQLStore(t)
	manager := NewDeviceManager(container, nil, nil)
	ad := types.NewADJID("6281777000098", types.WhatsAppDomain, 28)
	first := whatsmeow.NewClient(newTestStoreDevice(container, ad, ""), nil)
	second := whatsmeow.NewClient(newTestStoreDevice(container, types.NewADJID(ad.User, types.WhatsAppDomain, 32), ""), nil)
	a, b := NewDeviceInstance("a", first, nil), NewDeviceInstance("b", second, nil)
	manager.AddDevice(a)
	manager.AddDevice(b)
	dispatch := manager.eventHandler(first)
	dispatch(&events.PairPasskeyConfirmation{Code: "first"})
	_, code, _ := a.PasskeyState()
	require.Equal(t, "first", code)
	_, code, _ = b.PasskeyState()
	require.Empty(t, code)
	require.Len(t, websocket.Broadcast, 1)
	a.ClearPasskeyState()
	replacement := whatsmeow.NewClient(newTestStoreDevice(container, ad, ""), nil)
	manager.mu.Lock()
	manager.attachClientLocked(a, replacement)
	manager.mu.Unlock()
	dispatch(&events.PairPasskeyConfirmation{Code: "obsolete"})
	_, code, _ = a.PasskeyState()
	require.Empty(t, code)
	require.Len(t, websocket.Broadcast, 1)
	b.SetClient(replacement)
	manager.eventHandler(replacement)(&events.PairPasskeyConfirmation{Code: "ambiguous"})
	require.Len(t, websocket.Broadcast, 1)
}

func TestClientEventsEnsureDefaultTransferAndSameID(t *testing.T) {
	eventTestBroadcast(t)
	container := newTestSQLStore(t)
	manager := NewDeviceManager(container, nil, nil)
	ad := types.NewADJID("6281777000097", types.WhatsAppDomain, 28)
	client := whatsmeow.NewClient(newTestStoreDevice(container, ad, ""), nil)
	destination := NewDeviceInstance("named", nil, nil)
	manager.applyStoreJID(destination, ad)
	manager.AddDevice(destination)
	source := NewDeviceInstance(ad.String(), client, nil)
	manager.EnsureDefault(source)
	require.Same(t, destination, manager.eventOwner(client))
	require.Nil(t, source.GetClient())
	manager.eventHandler(client)(&events.PairPasskeyConfirmation{Code: "transferred"})
	require.Equal(t, "named", (<-websocket.Broadcast).Result.(map[string]any)["device_id"])
	discarded := whatsmeow.NewClient(newTestStoreDevice(container, ad, ""), nil)
	manager.EnsureDefault(NewDeviceInstance("named", discarded, nil))
	require.Nil(t, manager.eventOwner(discarded))
	require.Same(t, destination, manager.eventOwner(client))
	destination.TriggerLoggedOut()
	require.Nil(t, destination.GetClient())
	require.Empty(t, destination.JID())
}

func TestClientEventsUnpairedNamedSlot(t *testing.T) {
	eventTestBroadcast(t)
	container := newTestSQLStore(t)
	manager := NewDeviceManager(container, nil, nil)
	oldOS, oldPlatform := store.DeviceProps.Os, store.DeviceProps.PlatformType
	t.Cleanup(func() { store.DeviceProps.Os, store.DeviceProps.PlatformType = oldOS, oldPlatform })
	ctx, cancel := context.WithCancel(context.Background())
	instance, err := manager.EnsureClient(ctx, "unpaired")
	require.NoError(t, err)
	cancel()
	client := instance.GetClient()
	require.Nil(t, client.Store.ID)
	manager.eventHandler(client)(&events.PairPasskeyConfirmation{Code: "paired"})
	require.Equal(t, "unpaired", (<-websocket.Broadcast).Result.(map[string]any)["device_id"])
	require.Same(t, instance, manager.eventOwner(client))
}

func TestClientEventsTransientStartupAndRetirement(t *testing.T) {
	eventTestBroadcast(t)
	container := newTestSQLStore(t)
	storage := &keepSlotStubStorage{}
	globalStateMu.Lock()
	oldManager, oldClient, oldDB, oldKeys := deviceManager, cli, db, keysDB
	deviceManager = nil
	globalStateMu.Unlock()
	oldOS, oldPlatform := store.DeviceProps.Os, store.DeviceProps.PlatformType
	t.Cleanup(func() {
		store.DeviceProps.Os, store.DeviceProps.PlatformType = oldOS, oldPlatform
		globalStateMu.Lock()
		deviceManager, cli, db, keysDB = oldManager, oldClient, oldDB, oldKeys
		globalStateMu.Unlock()
	})
	client := InitWaCLI(context.Background(), container, nil, storage)
	manager := GetDeviceManager()
	require.Empty(t, manager.ListDevices())
	require.Nil(t, manager.DefaultDevice())
	owner := manager.eventOwner(client)
	require.NotNil(t, owner)
	manager.eventHandler(client)(&events.PairPasskeyConfirmation{Code: "anonymous"})
	require.Len(t, websocket.Broadcast, 1)
	ad := types.NewADJID("6281777000096", types.WhatsAppDomain, 28)
	client.Store.ID = &ad
	manager.eventHandler(client)(&events.Connected{})
	require.Empty(t, storage.savedRecords)
	require.Empty(t, manager.ListDevices())
	named := NewDeviceInstance("named", client, nil)
	manager.EnsureDefault(named)
	require.Nil(t, manager.transient)
	require.Nil(t, owner.GetClient())
	require.Same(t, named, manager.eventOwner(client))
	manager.eventHandler(client)(&events.PairPasskeyConfirmation{Code: "named"})
	require.Len(t, websocket.Broadcast, 2)
	require.Len(t, manager.ListDevices(), 1)
}

func TestClientEventsRegistryPublicationHasNoOwnershipGap(t *testing.T) {
	container := newTestSQLStore(t)
	manager := NewDeviceManager(container, nil, nil)
	ad := types.NewADJID("6281777000095", types.WhatsAppDomain, 28)
	client := whatsmeow.NewClient(newTestStoreDevice(container, ad, ""), nil)
	manager.EnsureDefault(NewDeviceInstance(ad.String(), client, nil))
	done := make(chan struct{})
	missing := make(chan bool, 1)
	go func() {
		for {
			select {
			case <-done:
				missing <- false
				return
			default:
				if manager.eventOwner(client) == nil {
					missing <- true
					return
				}
			}
		}
	}()
	for i := 0; i < 100; i++ {
		manager.loadFromRegistry([]*domainChatStorage.DeviceRecord{{DeviceID: "named", JID: ad.ToNonAD().String(), ADJID: ad.String()}})
	}
	close(done)
	require.False(t, <-missing)
	require.Equal(t, "named", manager.eventOwner(client).ID())
}

func TestClientEventsTransientRegistryAdoptionAndLogout(t *testing.T) {
	eventTestBroadcast(t)
	container := newTestSQLStore(t)
	manager := NewDeviceManager(container, nil, nil)
	client := whatsmeow.NewClient(container.NewDevice(), nil)
	transient := NewDeviceInstance("", client, nil)
	manager.EnsureDefault(transient)
	ad := types.NewADJID("6281777000094", types.WhatsAppDomain, 28)
	client.Store.ID = &ad
	manager.eventHandler(client)(&events.PairPasskeyConfirmation{Code: "pairing"})
	manager.loadFromRegistry([]*domainChatStorage.DeviceRecord{{DeviceID: "named", JID: ad.ToNonAD().String(), ADJID: ad.String()}})
	require.Nil(t, manager.transient)
	require.Nil(t, transient.GetClient())
	require.Equal(t, "named", manager.eventOwner(client).ID())
	manager.eventOwner(client).TriggerLoggedOut()
	require.Nil(t, manager.eventOwner(client))

	fresh := whatsmeow.NewClient(container.NewDevice(), nil)
	manager.EnsureDefault(NewDeviceInstance("", fresh, nil))
	manager.eventHandler(fresh)(&events.LoggedOut{})
	require.Nil(t, manager.transient)
	require.Nil(t, manager.eventOwner(fresh))
	require.Len(t, manager.ListDevices(), 1)
}
