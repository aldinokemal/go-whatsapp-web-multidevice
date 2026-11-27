package whatsapp

import (
	"context"
	"errors"
	"maps"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	routedPhone = "628111@s.whatsapp.net"
	routedLID   = "123@lid"
)

// routedDevice returns a device whose client holds lids as its LID mapping
// store, registered in a fresh global device manager.
func routedDevice(t *testing.T, id string, lids store.LIDStore) *DeviceInstance {
	t.Helper()
	instance := NewDeviceInstance(id, &whatsmeow.Client{Store: &store.Device{LIDs: lids}}, nil)
	if deviceManager == nil {
		t.Fatal("routedDevice needs withRoutedDeviceManager")
	}
	deviceManager.AddDevice(instance)
	return instance
}

func withRoutedDeviceManager(t *testing.T) {
	t.Helper()
	orig := deviceManager
	origGlobal := config.ChatwootIgnoreJids
	t.Cleanup(func() {
		deviceManager = orig
		config.ChatwootIgnoreJids = origGlobal
	})
	deviceManager = NewDeviceManager(nil, nil, nil)
	config.ChatwootIgnoreJids = nil
}

// A message queued while the device knew only the phone number is replayed
// after the device has learned 628111 = 123@lid. The device's ignore entry for
// 123@lid must hold for the replay, though the frozen payload is phone-only;
// another device without that mapping still forwards the same payload.
func TestQueuedRetryHonorsLIDMappingLearnedAfterEnqueue(t *testing.T) {
	withRoutedDeviceManager(t)
	log := &chatwootRequestLog{}
	srv := httptest.NewServer(log)
	t.Cleanup(srv.Close)
	deviceIgnore := []string{routedLID}
	stubChatwootDevice(t, srv, &deviceIgnore)

	mappings := &chatDisplayNameLIDStore{} // nothing known at enqueue time
	routedDevice(t, "dev-routed", mappings)
	routedDevice(t, "dev-other", &chatDisplayNameLIDStore{})

	payload := map[string]any{"event": "message", "payload": map[string]any{
		"id": "wa-queued", "chat_id": routedPhone, "from": routedPhone, "from_name": "Ana", "body": "hello",
	}}
	queue := &chatwootForwardQueueTestRepo{}
	for _, device := range []string{"dev-routed", "dev-other"} {
		require.True(t, enqueueChatwootForwardRetry(queue, device, "message", payload, errors.New("chatwoot timeout")))
	}
	require.NotContains(t, queue.events[0].PayloadJSON, routedLID, "the queued payload must be phone-only")

	// The routed device learns the mapping after the event was queued.
	mappings.pn = types.NewJID("628111", types.DefaultUserServer)
	mappings.lid = types.NewJID("123", types.HiddenUserServer)

	log.take()
	err := processChatwootForwardRetryEvent(nil, queue.events[0])
	require.Empty(t, log.take(), "a replay of an ignored LID's message reached Chatwoot")
	require.NoError(t, err)

	// Device isolation: dev-other has no mapping, so 123@lid says nothing about
	// its phone-addressed chat, and the same payload is forwarded.
	_ = processChatwootForwardRetryEvent(nil, queue.events[1])
	require.NotEmpty(t, log.take(), "another device's mapping must not suppress this device's message")
}

// The real DeleteForMe payload carries the stored phone chat and the event's
// phone sender only. With ChatwootForwardDeletes on, the "deleted" note is new
// Chatwoot content and must be suppressed for a device ignoring 123@lid.
func TestDeleteForMeNoteHonorsRoutedDeviceLIDIgnore(t *testing.T) {
	withRoutedDeviceManager(t)
	prevDelete, prevNotes := config.ChatwootMessageDelete, config.ChatwootForwardDeletes
	t.Cleanup(func() { config.ChatwootMessageDelete, config.ChatwootForwardDeletes = prevDelete, prevNotes })
	config.ChatwootMessageDelete = true
	config.ChatwootForwardDeletes = true

	log := &chatwootRequestLog{}
	srv := httptest.NewServer(log)
	t.Cleanup(srv.Close)
	var deviceIgnore []string
	stubChatwootDevice(t, srv, &deviceIgnore)

	phone := types.NewJID("628111", types.DefaultUserServer)
	instance := routedDevice(t, "dev-routed", &chatDisplayNameLIDStore{pn: phone, lid: types.NewJID("123", types.HiddenUserServer)})
	ctx := ContextWithDevice(context.Background(), instance)

	evt := &events.DeleteForMe{ChatJID: phone, SenderJID: phone, MessageID: "wa-deleted", Timestamp: time.Now()}
	stored := &domainChatStorage.Message{
		ID: "wa-deleted", ChatJID: routedPhone, Sender: routedPhone, Content: "secret", Timestamp: time.Now(),
	}
	payload, err := createDeletePayload(ctx, evt, stored, "dev-routed", instance.GetClient())
	require.NoError(t, err)
	data := payload["payload"].(map[string]any)
	require.NotContains(t, data, "chat_lid", "the DeleteForMe payload must be phone-only for this regression")
	require.NotContains(t, data, "from_lid", "the DeleteForMe payload must be phone-only for this regression")

	repo := &ignoreLinkRepo{links: map[string]*domainChatStorage.ChatwootMessageLink{}}

	// Without an ignore entry the note is posted, so the next check is meaningful.
	log.take()
	_ = syncPayloadToChatwoot(ctx, payload, "message.deleted", "dev-routed", repo)
	require.NotEmpty(t, log.take(), "the delete note should reach Chatwoot without an ignore entry")

	deviceIgnore = []string{routedLID}
	err = syncPayloadToChatwoot(ctx, payload, "message.deleted", "dev-routed", repo)
	require.Empty(t, log.take(), "a DeleteForMe note for an ignored LID reached Chatwoot")
	require.NoError(t, err)
}

type erroringLIDStore struct{ store.NoopStore }

func (erroringLIDStore) GetLIDForPN(context.Context, types.JID) (types.JID, error) {
	return types.EmptyJID, errors.New("database is locked")
}

func (erroringLIDStore) GetPNForLID(context.Context, types.JID) (types.JID, error) {
	return types.EmptyJID, errors.New("database is locked")
}

// A mapping that cannot be read while an ignore list is in force fails closed
// with a retryable error: no request is made, and the live path queues the
// event instead of mirroring a chat it may have been told to ignore. Without
// an ignore list there is nothing to check and no lookup is made.
func TestRoutedIdentityLookupFailureDefersForward(t *testing.T) {
	withRoutedDeviceManager(t)
	log := &chatwootRequestLog{}
	srv := httptest.NewServer(log)
	t.Cleanup(srv.Close)
	deviceIgnore := []string{routedLID}
	stubChatwootDevice(t, srv, &deviceIgnore)

	ctx := ContextWithDevice(context.Background(), routedDevice(t, "dev-routed", &erroringLIDStore{}))
	payload := func() map[string]any {
		return map[string]any{"payload": map[string]any{
			"id": "wa-live", "chat_id": routedPhone, "from": routedPhone, "from_name": "Ana", "body": "hello",
		}}
	}

	log.take()
	err := syncPayloadToChatwoot(ctx, payload(), "message", "dev-routed", nil)
	require.ErrorIs(t, err, errChatwootIdentityUnavailable)
	require.Empty(t, log.take())

	queue := &chatwootForwardQueueTestRepo{}
	require.True(t, enqueueChatwootForwardRetry(queue, "dev-routed", "message", payload(), err), "the deferred event must be queued")

	// A routed device without a client cannot be checked either.
	noClient := NewDeviceInstance("dev-offline", nil, nil)
	err = syncPayloadToChatwoot(ContextWithDevice(context.Background(), noClient), payload(), "message", "dev-offline", nil)
	require.ErrorIs(t, err, errChatwootIdentityUnavailable)
	require.Empty(t, log.take())

	deviceIgnore = nil
	_ = syncPayloadToChatwoot(ctx, payload(), "message", "dev-routed", nil)
	require.NotEmpty(t, log.take(), "without an ignore list the failing store must not block forwarding")
}

func TestChatwootRoutedIdentityPreservesSuppliedForms(t *testing.T) {
	phone := types.NewJID("628111", types.DefaultUserServer)
	instance := NewDeviceInstance("dev", &whatsmeow.Client{Store: &store.Device{LIDs: &chatDisplayNameLIDStore{
		pn: phone, lid: types.NewJID("123", types.HiddenUserServer),
	}}}, nil)
	ctx := ContextWithDevice(context.Background(), instance)
	ignore := []string{"999@lid"}

	for _, tt := range []struct {
		name                         string
		data                         map[string]any
		chat, chatAlias, from, alias string
	}{
		{"phone-only payload gains the known lid", map[string]any{"chat_id": routedPhone, "from": routedPhone},
			routedPhone, routedLID, routedPhone, routedLID},
		{"unresolved lid gains the known phone", map[string]any{"chat_id": routedLID, "chat_lid": routedLID, "from": routedLID},
			routedLID, routedPhone, routedLID, routedPhone},
		{"supplied alias is kept", map[string]any{"chat_id": routedPhone, "chat_lid": "456@lid", "from": routedPhone, "from_lid": "456@lid"},
			routedPhone, "456@lid", routedPhone, "456@lid"},
		{"group chat is left alone", map[string]any{"chat_id": "120363000000000001@g.us", "from": routedPhone},
			"120363000000000001@g.us", "", routedPhone, routedLID},
		{"unknown contact stays unresolved", map[string]any{"chat_id": "628999@s.whatsapp.net", "from": "628999@s.whatsapp.net"},
			"628999@s.whatsapp.net", "", "628999@s.whatsapp.net", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := maps.Clone(tt.data)
			id, err := chatwootRoutedIdentity(ctx, tt.data, ignore)
			require.NoError(t, err)
			require.Equal(t, tt.chat, id.ChatJID)
			require.Equal(t, tt.chatAlias, id.ChatLID)
			require.Equal(t, tt.from, id.SenderJID)
			require.Equal(t, tt.alias, id.SenderLID)
			require.Equal(t, before, tt.data, "the payload itself is not rewritten")
		})
	}

	// No routed device: nothing is looked up, and the global client is not used.
	id, err := chatwootRoutedIdentity(context.Background(), map[string]any{"chat_id": routedPhone, "from": routedPhone}, ignore)
	require.NoError(t, err)
	require.Empty(t, id.ChatLID)
	require.Empty(t, id.SenderLID)
}
