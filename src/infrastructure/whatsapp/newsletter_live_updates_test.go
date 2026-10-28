package whatsapp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
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
