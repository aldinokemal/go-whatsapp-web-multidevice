package whatsapp

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

type fakeLiveUpdatesClient struct {
	mu        sync.Mutex
	connected bool
	account   string
	duration  time.Duration
	err       error
	calls     int
	called    chan struct{}
	// block, when set, holds NewsletterSubscribeLiveUpdates until it is closed.
	block chan struct{}
}

func newFakeLiveUpdatesClient(duration time.Duration) *fakeLiveUpdatesClient {
	return &fakeLiveUpdatesClient{connected: true, account: "628123@s.whatsapp.net", duration: duration, called: make(chan struct{}, 16)}
}

func (f *fakeLiveUpdatesClient) IsConnected() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connected
}

func (f *fakeLiveUpdatesClient) NewsletterSubscribeLiveUpdates(context.Context, types.JID) (time.Duration, error) {
	f.mu.Lock()
	f.calls++
	duration, err, block := f.duration, f.err, f.block
	f.mu.Unlock()
	f.called <- struct{}{}
	if block != nil {
		<-block
	}
	return duration, err
}

func (f *fakeLiveUpdatesClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func sourceOf(client *fakeLiveUpdatesClient) liveUpdatesClientSource {
	return func() (liveUpdatesClient, string) {
		client.mu.Lock()
		defer client.mu.Unlock()
		return client, client.account
	}
}

func waitCall(t *testing.T, client *fakeLiveUpdatesClient) {
	t.Helper()
	select {
	case <-client.called:
	case <-time.After(2 * time.Second):
		t.Fatal("expected a live updates subscription call")
	}
}

var testNewsletterJID = types.NewJID("120363123456789", types.NewsletterServer)

func TestLiveUpdatesSubscribeRecordsGrant(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	client := newFakeLiveUpdatesClient(5 * time.Minute)

	status, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
	require.NoError(t, err)
	defer manager.stopDevice("device-a")

	assert.Equal(t, testNewsletterJID, status.NewsletterJID)
	assert.Equal(t, 5*time.Minute, status.Duration)
	assert.Equal(t, now.Add(5*time.Minute), status.ExpiresAt)
	require.Len(t, manager.list("device-a"), 1)
	assert.Empty(t, manager.list("device-b"))
}

func TestLiveUpdatesSubscribeRejectsDisconnectedDevice(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	client := newFakeLiveUpdatesClient(time.Minute)
	client.connected = false

	_, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
	require.EqualError(t, err, "device device-a is not connected")
	assert.Empty(t, manager.list("device-a"))

	_, err = manager.subscribe(context.Background(), "device-a", testNewsletterJID, func() (liveUpdatesClient, string) { return nil, "" })
	require.Error(t, err)
}

func TestLiveUpdatesSubscribeDoesNotRegisterFailedGrant(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	client := newFakeLiveUpdatesClient(time.Minute)
	client.err = errors.New("forbidden")

	_, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
	require.ErrorContains(t, err, "forbidden")
	assert.Empty(t, manager.list("device-a"))
}

func TestLiveUpdatesRenewDeviceRenewsImmediately(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	client := newFakeLiveUpdatesClient(time.Hour)

	_, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
	require.NoError(t, err)
	defer manager.stopDevice("device-a")
	waitCall(t, client)

	manager.renewDevice("device-a")
	waitCall(t, client)
	assert.Equal(t, 2, client.callCount())
}

func TestLiveUpdatesRenewsBeforeExpiry(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	client := newFakeLiveUpdatesClient(liveUpdatesMinRenewDelay)
	start := time.Now()
	manager.now = func() time.Time { return start }

	_, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
	require.NoError(t, err)
	defer manager.stopDevice("device-a")
	waitCall(t, client)

	select {
	case <-client.called:
	case <-time.After(liveUpdatesMinRenewDelay + 3*time.Second):
		t.Fatal("expected the subscription to be renewed before it expires")
	}
}

func TestLiveUpdatesUnsubscribeStopsRenewal(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	client := newFakeLiveUpdatesClient(time.Hour)

	_, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
	require.NoError(t, err)

	assert.True(t, manager.unsubscribe("device-a", testNewsletterJID))
	assert.False(t, manager.unsubscribe("device-a", testNewsletterJID))
	assert.Empty(t, manager.list("device-a"))
}

func TestLiveUpdatesRenewalFailureIsReported(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	client := newFakeLiveUpdatesClient(time.Hour)

	_, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
	require.NoError(t, err)
	defer manager.stopDevice("device-a")
	waitCall(t, client)

	client.mu.Lock()
	client.connected = false
	client.mu.Unlock()
	manager.renewDevice("device-a")

	require.Eventually(t, func() bool {
		statuses := manager.list("device-a")
		return len(statuses) == 1 && statuses[0].LastError == "device is not connected"
	}, 2*time.Second, 10*time.Millisecond)
}

func TestLiveUpdatesBackoff(t *testing.T) {
	assert.Equal(t, liveUpdatesRetryDelay, liveUpdatesBackoff(1))
	assert.Equal(t, 2*liveUpdatesRetryDelay, liveUpdatesBackoff(2))
	assert.Equal(t, liveUpdatesMaxRetryDelay, liveUpdatesBackoff(20))
}

func TestLiveUpdatesStopWhenAccountChanges(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	client := newFakeLiveUpdatesClient(time.Hour)

	_, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
	require.NoError(t, err)
	waitCall(t, client)

	// Another account is paired in the same slot: the old subscription must
	// end instead of being renewed through the new account.
	client.mu.Lock()
	client.account = "628999@s.whatsapp.net"
	client.mu.Unlock()
	manager.renewDevice("device-a")

	require.Eventually(t, func() bool { return len(manager.list("device-a")) == 0 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, client.callCount())
}

func TestLiveUpdatesDiscardSubscribeFinishedAfterStop(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	client := newFakeLiveUpdatesClient(time.Hour)
	client.block = make(chan struct{})

	result := make(chan error, 1)
	go func() {
		_, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
		result <- err
	}()
	waitCall(t, client)

	manager.stopDevice("device-a")
	close(client.block)

	select {
	case err := <-result:
		require.ErrorContains(t, err, "logged out or removed while subscribing")
	case <-time.After(2 * time.Second):
		t.Fatal("subscribe did not return")
	}
	assert.Empty(t, manager.list("device-a"))
}

func TestLiveUpdatesResubscribeAfterStop(t *testing.T) {
	manager := newNewsletterLiveUpdatesManager()
	client := newFakeLiveUpdatesClient(time.Hour)

	manager.stopDevice("device-a")
	_, err := manager.subscribe(context.Background(), "device-a", testNewsletterJID, sourceOf(client))
	require.NoError(t, err)
	defer manager.stopDevice("device-a")
	assert.Len(t, manager.list("device-a"), 1)
}

func TestLiveUpdatesConnectedRenewsReconciledStartupSlot(t *testing.T) {
	ctx := context.Background()
	container := newTestSQLStore(t)
	adA := types.NewADJID("6281777000099", types.WhatsAppDomain, 28)
	adB := types.NewADJID(adA.User, types.WhatsAppDomain, 32)
	require.NoError(t, newTestStoreDevice(container, adA, "").Save(ctx))
	storage := &registryStubStorage{records: []*domainChatStorage.DeviceRecord{
		{DeviceID: "slot-a", JID: adA.ToNonAD().String(), ADJID: adA.String()},
		{DeviceID: "slot-b", JID: adB.ToNonAD().String(), ADJID: adB.String()},
	}}
	globalStateMu.Lock()
	oldManager, oldClient, oldDB, oldKeys := deviceManager, cli, db, keysDB
	deviceManager = nil
	globalStateMu.Unlock()
	oldNewsletter := newsletterLiveUpdates
	newsletterLiveUpdates = newNewsletterLiveUpdatesManager()
	oldOS, oldPlatform := store.DeviceProps.Os, store.DeviceProps.PlatformType
	oldChatwoot := config.ChatwootEnabled
	config.ChatwootEnabled = false
	t.Cleanup(func() {
		newsletterLiveUpdates.stopDevice("slot-a")
		newsletterLiveUpdates.stopDevice("slot-b")
		newsletterLiveUpdates = oldNewsletter
		config.ChatwootEnabled = oldChatwoot
		store.DeviceProps.Os, store.DeviceProps.PlatformType = oldOS, oldPlatform
		globalStateMu.Lock()
		deviceManager, cli, db, keysDB = oldManager, oldClient, oldDB, oldKeys
		globalStateMu.Unlock()
	})
	client := InitWaCLI(ctx, container, nil, storage)
	dispatch := registeredNewsletterEventHandler(t, client)
	require.NoError(t, newTestStoreDevice(container, adB, "").Save(ctx))
	manager := GetDeviceManager()
	startup, ok := manager.GetDevice(adA.String())
	require.True(t, ok)
	require.NoError(t, manager.LoadExistingDevices(ctx))
	current, ok := manager.GetDevice("slot-a")
	require.True(t, ok)
	require.NotSame(t, startup, current)
	require.Same(t, client, current.GetClient())
	sibling, err := manager.EnsureClient(ctx, "slot-b")
	require.NoError(t, err)
	siblingDispatch := registeredNewsletterEventHandler(t, sibling.GetClient())
	fakeA, fakeB := newFakeLiveUpdatesClient(time.Hour), newFakeLiveUpdatesClient(time.Hour)
	for slot, fake := range map[string]*fakeLiveUpdatesClient{"slot-a": fakeA, "slot-b": fakeB} {
		_, err := newsletterLiveUpdates.subscribe(ctx, slot, testNewsletterJID, sourceOf(fake))
		require.NoError(t, err)
		waitCall(t, fake)
	}
	dispatch(&events.Connected{})
	waitCall(t, fakeA)
	assert.Equal(t, 2, fakeA.callCount())
	assert.Equal(t, 1, fakeB.callCount())

	dispatch(&events.PushNameSetting{})
	select {
	case <-fakeA.called:
		t.Fatal("pushname update must not renew subscriptions")
	case <-time.After(50 * time.Millisecond):
	}

	siblingDispatch(&events.Connected{})
	waitCall(t, fakeB)
	assert.Equal(t, 2, fakeB.callCount())
	assert.Equal(t, 2, fakeA.callCount())

	current.SetClient(whatsmeow.NewClient(newTestStoreDevice(container, adA, ""), nil))
	dispatch(&events.Connected{})
	select {
	case <-fakeA.called:
		t.Fatal("obsolete client must not renew replacement client's subscriptions")
	case <-fakeB.called:
		t.Fatal("obsolete client must not renew sibling subscriptions")
	case <-time.After(50 * time.Millisecond):
	}
}

// whatsmeow does not expose dispatch. Read the registered callback to verify the
// production registration, without opening a connection or recreating its closure.
func registeredNewsletterEventHandler(t *testing.T, client *whatsmeow.Client) func(any) {
	t.Helper()
	handlers := reflect.ValueOf(client).Elem().FieldByName("eventHandlers")
	require.True(t, handlers.IsValid())
	require.Equal(t, 1, handlers.Len(), "client must register exactly one event handler")
	field := handlers.Index(0).FieldByName("fn")
	require.True(t, field.IsValid())
	require.Equal(t, reflect.TypeOf((whatsmeow.EventHandlerWithSuccessStatus)(nil)), field.Type())
	callback := *(*whatsmeow.EventHandlerWithSuccessStatus)(unsafe.Pointer(field.UnsafeAddr()))
	return func(event any) { require.True(t, callback(event)) }
}
