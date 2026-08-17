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

// viewOnceFakeRepo overrides only the methods persistViewOncePlaceholder
// touches; anything else panics via the embedded nil interface.
type viewOnceFakeRepo struct {
	domainChatStorage.IChatStorageRepository
	storedChat    *domainChatStorage.Chat
	storedMessage *domainChatStorage.Message
	chatName      string
}

// The device-scoped wrapper resolves GetChat/name via the *ByDevice variants
// on its base, injecting its own key — the fake implements those.
func (r *viewOnceFakeRepo) GetChatByDevice(deviceID, jid string) (*domainChatStorage.Chat, error) {
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

	if envelope["event"] != EventTypeMessage {
		t.Fatalf("event = %v, want %q", envelope["event"], EventTypeMessage)
	}
	payload, ok := envelope["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload is %T, want map[string]any", envelope["payload"])
	}

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
		if payload[key] != expected {
			t.Errorf("payload[%q] = %v, want %v", key, payload[key], expected)
		}
	}
	if _, hasBody := payload["body"]; hasBody {
		t.Errorf("payload must not carry a body: the content was never delivered")
	}
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

// Persistence must go through the device-scoped wrapper with an EMPTY
// DeviceID (the wrapper injects its partition key), leave media_type empty,
// and resolve the chat name via the shared resolver — never a raw PushName.
func TestPersistViewOncePlaceholder(t *testing.T) {
	fake := &viewOnceFakeRepo{chatName: "resolved-name"}
	repo := newDeviceChatStorage("device-alias", fake)

	persistViewOncePlaceholder(context.Background(), viewOnceEvent(), repo, nil)

	if fake.storedChat == nil || fake.storedMessage == nil {
		t.Fatalf("chat/message not persisted: %+v %+v", fake.storedChat, fake.storedMessage)
	}
	if fake.storedChat.DeviceID != "device-alias" || fake.storedMessage.DeviceID != "device-alias" {
		t.Fatalf("partition mismatch: chat=%q message=%q (both must be the wrapper's key)",
			fake.storedChat.DeviceID, fake.storedMessage.DeviceID)
	}
	if fake.storedChat.Name != "resolved-name" {
		t.Fatalf("chat name must come from the shared resolver, got %q", fake.storedChat.Name)
	}
	if fake.storedMessage.MediaType != "" {
		t.Fatalf("placeholder must not carry a media_type, got %q", fake.storedMessage.MediaType)
	}
	if fake.storedMessage.Content != viewOncePlaceholderNotice {
		t.Fatalf("unexpected content %q", fake.storedMessage.Content)
	}
}

// The event-handler closure can hold a long-canceled login request context;
// persistence must still happen (the handler detaches a bounded context).
func TestHandleUndecryptableMessagePersistsUnderCanceledContext(t *testing.T) {
	ensureTestLogger(t)
	fake := &viewOnceFakeRepo{chatName: "n"}
	repo := newDeviceChatStorage("device-alias", fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled, like a finished login request

	handleUndecryptableMessage(ctx, viewOnceEvent(), repo, nil)

	if fake.storedMessage == nil {
		t.Fatalf("placeholder was not persisted under a canceled parent context")
	}
}
