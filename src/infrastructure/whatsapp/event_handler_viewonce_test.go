package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// The package logger is only wired up at runtime (database.go); the handler
// under test logs unconditionally. Restores the previous value on cleanup so
// no test state leaks (tests that mutate package globals restore them).
func ensureTestLogger(t *testing.T) {
	t.Helper()
	prev := log
	if log == nil {
		log = waLog.Stdout("test", "ERROR", false)
	}
	t.Cleanup(func() { log = prev })
}

// loggedInJID is the device JID the fake client reports as logged in. It
// deliberately differs from the alias handed to the device-scoped wrapper in
// these tests: the placeholder must be partitioned by the logged-in JID, which
// is what CreateMessage and the REST chat queries use.
const loggedInJID = "628987654321@s.whatsapp.net"

// fakeLoggedInClient builds the minimum client the placeholder path reads: a
// store carrying the logged-in device JID (with a device suffix, to prove it is
// stripped).
func fakeLoggedInClient() *whatsmeow.Client {
	jid := types.NewJID("628987654321", types.DefaultUserServer)
	jid.Device = 3
	return &whatsmeow.Client{Store: &store.Device{ID: &jid}}
}

// viewOnceFakeRepo overrides only the methods persistViewOncePlaceholder
// touches; anything else panics via the embedded nil interface.
type viewOnceFakeRepo struct {
	domainChatStorage.IChatStorageRepository
	storedChat    *domainChatStorage.Chat
	storedMessage *domainChatStorage.Message
	chatName      string
	// nameDeviceID / getChatDeviceID record the partition key the resolver and
	// the existing-chat lookup were asked about.
	nameDeviceID    string
	getChatDeviceID string
}

// The placeholder path addresses the repository through the *ByDevice variants
// with an explicit device id — the fake implements those.
func (r *viewOnceFakeRepo) GetChatByDevice(deviceID, jid string) (*domainChatStorage.Chat, error) {
	r.getChatDeviceID = deviceID
	return nil, nil
}
func (r *viewOnceFakeRepo) StoreChat(chat *domainChatStorage.Chat) error {
	r.storedChat = chat
	return nil
}
func (r *viewOnceFakeRepo) StoreMessage(message *domainChatStorage.Message) error {
	r.storedMessage = message
	return nil
}
func (r *viewOnceFakeRepo) GetChatNameWithPushNameByDevice(deviceID string, jid types.JID, chatJID string, senderUser string, pushName string) string {
	r.nameDeviceID = deviceID
	return r.chatName
}

// A view-once "unavailable" placeholder (WhatsApp withholds view-once content
// from linked devices) must surface as a `message` event with view_once and
// unavailable flags and the regular from/chat_id fields, so consumers can
// render the same notice WhatsApp Web shows.
func TestBuildViewOncePlaceholderEvent(t *testing.T) {
	ts := time.Date(2023, 10, 15, 11, 41, 0, 0, time.UTC)
	evt := &events.UndecryptableMessage{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("628123456789", types.DefaultUserServer),
				Sender:   types.NewJID("628123456789", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "3EB0C127D7BACC83D6B9",
			Timestamp: ts,
			PushName:  "John Doe",
		},
		IsUnavailable:   true,
		UnavailableType: events.UnavailableTypeViewOnce,
	}

	envelope := buildViewOncePlaceholderEvent(context.Background(), nil, evt)

	assert.Equal(t, EventTypeMessage, envelope["event"])
	payload, ok := envelope["payload"].(map[string]any)
	require.True(t, ok, "payload is %T, want map[string]any", envelope["payload"])

	want := map[string]any{
		"id":          "3EB0C127D7BACC83D6B9",
		"timestamp":   ts.Format(time.RFC3339),
		"is_from_me":  false,
		"view_once":   true,
		"unavailable": true,
		"chat_id":     "628123456789@s.whatsapp.net",
		"from":        "628123456789@s.whatsapp.net",
		"from_name":   "John Doe",
	}
	for key, expected := range want {
		assert.Equal(t, expected, payload[key], "payload[%q]", key)
	}
	assert.NotContains(t, payload, "body", "the content was never delivered")
}

func viewOnceEvent() *events.UndecryptableMessage {
	return &events.UndecryptableMessage{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("628123456789", types.DefaultUserServer),
				Sender:   types.NewJID("628123456789", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "3EB0C127D7BACC83D6C1",
			Timestamp: time.Date(2023, 10, 15, 11, 41, 0, 0, time.UTC),
			PushName:  "John Doe",
		},
		IsUnavailable:   true,
		UnavailableType: events.UnavailableTypeViewOnce,
	}
}

// The partition key is the logged-in JID, never the registration alias the
// device-scoped wrapper may still carry. Anything short of a logged-in client
// yields "" so the caller skips persistence instead of guessing.
func TestPlaceholderDeviceID(t *testing.T) {
	loggedIn := fakeLoggedInClient()
	cases := []struct {
		name   string
		client *whatsmeow.Client
		want   string
	}{
		{"nil client", nil, ""},
		{"no store", &whatsmeow.Client{}, ""},
		{"store without id (not logged in)", &whatsmeow.Client{Store: &store.Device{}}, ""},
		{"logged in: device suffix stripped", loggedIn, loggedInJID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, placeholderDeviceID(tc.client))
		})
	}
}

// Persistence must be partitioned by the LOGGED-IN JID (not the wrapper's
// alias, whose partition nothing queries), carry the view_once_unavailable
// discriminator so history importers can flag the Chatwoot link, and resolve
// the chat name via the shared device-scoped resolver — never a raw PushName.
func TestPersistViewOncePlaceholder(t *testing.T) {
	fake := &viewOnceFakeRepo{chatName: "resolved-name"}
	repo := newDeviceChatStorage("device-alias", fake)

	persistViewOncePlaceholder(context.Background(), viewOnceEvent(), repo, fakeLoggedInClient())

	require.NotNil(t, fake.storedChat, "chat not persisted")
	require.NotNil(t, fake.storedMessage, "message not persisted")
	assert.Equal(t, loggedInJID, fake.storedChat.DeviceID)
	assert.Equal(t, loggedInJID, fake.storedMessage.DeviceID)
	assert.Equal(t, loggedInJID, fake.nameDeviceID, "name resolver must be asked about the logged-in partition")
	assert.Equal(t, loggedInJID, fake.getChatDeviceID, "existing-chat lookup must read the logged-in partition")
	assert.Equal(t, "resolved-name", fake.storedChat.Name)
	assert.Equal(t, domainChatStorage.MediaTypeViewOnceUnavailable, fake.storedMessage.MediaType)
	assert.Equal(t, viewOncePlaceholderNotice, fake.storedMessage.Content)
}

// Without a logged-in client there is no trustworthy partition key, so the
// placeholder is dropped rather than written somewhere nothing reads. The
// webhook forward still happens; only storage is skipped.
func TestPersistViewOncePlaceholderWithoutClientIsNoOp(t *testing.T) {
	fake := &viewOnceFakeRepo{chatName: "resolved-name"}
	repo := newDeviceChatStorage("device-alias", fake)

	persistViewOncePlaceholder(context.Background(), viewOnceEvent(), repo, nil)

	assert.Nil(t, fake.storedChat)
	assert.Nil(t, fake.storedMessage)
}

// The event-handler closure can hold a long-canceled login request context;
// persistence must still happen (the handler detaches a bounded context).
func TestHandleUndecryptableMessagePersistsUnderCanceledContext(t *testing.T) {
	ensureTestLogger(t)
	fake := &viewOnceFakeRepo{chatName: "n"}
	repo := newDeviceChatStorage("device-alias", fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled, like a finished login request

	handleUndecryptableMessage(ctx, viewOnceEvent(), repo, fakeLoggedInClient())

	assert.NotNil(t, fake.storedMessage, "placeholder was not persisted under a canceled parent context")
}
