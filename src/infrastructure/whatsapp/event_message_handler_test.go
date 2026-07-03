package whatsapp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func TestHandleMessageReactionStoresReactionAndForwardsWebhook(t *testing.T) {
	originalWebhookURLs := config.WhatsappWebhook
	originalWebhookEvents := config.WhatsappWebhookEvents
	originalAutoReply := config.WhatsappAutoReplyMessage
	originalAutoMarkRead := config.WhatsappAutoMarkRead
	originalSubmit := submitWebhookFn
	originalLog := log
	defer func() {
		config.WhatsappWebhook = originalWebhookURLs
		config.WhatsappWebhookEvents = originalWebhookEvents
		config.WhatsappAutoReplyMessage = originalAutoReply
		config.WhatsappAutoMarkRead = originalAutoMarkRead
		submitWebhookFn = originalSubmit
		log = originalLog
	}()

	log = waLog.Noop
	config.WhatsappWebhook = []string{"https://example.test/webhook"}
	config.WhatsappWebhookEvents = nil
	config.WhatsappAutoReplyMessage = ""
	config.WhatsappAutoMarkRead = false

	repo := &messageHandlerRepoSpy{}
	done := make(chan map[string]any, 1)
	submitWebhookFn = func(_ context.Context, payload map[string]any, _ string, _ *domainChatStorage.DeviceWebhookConfig) error {
		done <- payload
		return nil
	}

	evt := reactionEventForTest("reaction-event-1", "msg-1", "\U0001f44d")
	handleMessage(context.Background(), evt, repo, nil)

	if got := repo.createReactionCount(); got != 1 {
		t.Fatalf("expected reaction path to call CreateReaction once, got %d", got)
	}
	if got := repo.createMessageCount(); got != 0 {
		t.Fatalf("expected reaction path not to call CreateMessage, got %d", got)
	}

	select {
	case payload := <-done:
		if got := payload["event"]; got != EventTypeMessageReaction {
			t.Fatalf("expected webhook event %q, got %v", EventTypeMessageReaction, got)
		}
		eventPayload, ok := payload["payload"].(map[string]any)
		if !ok {
			t.Fatalf("expected payload map, got %T", payload["payload"])
		}
		if got := eventPayload["reaction"]; got != "\U0001f44d" {
			t.Fatalf("expected reaction in webhook payload, got %v", got)
		}
		if got := eventPayload["reacted_message_id"]; got != "msg-1" {
			t.Fatalf("expected reacted message id, got %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook submission")
	}
}

func TestHandleWebhookForwardSkipsBroadcastRegardlessOfChatwoot(t *testing.T) {
	originalWebhookURLs := config.WhatsappWebhook
	originalWebhookEvents := config.WhatsappWebhookEvents
	originalChatwootEnabled := config.ChatwootEnabled
	originalSubmit := submitWebhookFn
	originalLog := log
	defer func() {
		config.WhatsappWebhook = originalWebhookURLs
		config.WhatsappWebhookEvents = originalWebhookEvents
		config.ChatwootEnabled = originalChatwootEnabled
		submitWebhookFn = originalSubmit
		log = originalLog
	}()

	log = waLog.Noop
	config.WhatsappWebhook = []string{"https://example.test/webhook"}
	config.WhatsappWebhookEvents = nil

	delivered := make(chan map[string]any, 8)
	submitWebhookFn = func(_ context.Context, payload map[string]any, _ string, _ *domainChatStorage.DeviceWebhookConfig) error {
		delivered <- payload
		return nil
	}

	// Broadcast/status messages must never reach webhooks, whether Chatwoot is
	// enabled or not: the Chatwoot pipeline rejects status@broadcast anyway,
	// and plain webhook consumers must not start receiving broadcast noise
	// just because Chatwoot is turned on (regression from PR #671).
	statusChat := types.NewJID("status", types.BroadcastServer)
	for _, chatwootEnabled := range []bool{false, true} {
		config.ChatwootEnabled = chatwootEnabled
		handleWebhookForward(context.Background(), textEventForTest("broadcast-1", statusChat), nil)
	}

	// Control: a regular DM must still be forwarded, so the guard is proven
	// to filter broadcasts specifically rather than everything.
	config.ChatwootEnabled = false
	dmChat := types.NewJID("628123456789", types.DefaultUserServer)
	handleWebhookForward(context.Background(), textEventForTest("dm-1", dmChat), nil)

	select {
	case payload := <-delivered:
		eventPayload, ok := payload["payload"].(map[string]any)
		if !ok {
			t.Fatalf("expected payload map, got %T", payload["payload"])
		}
		if got := eventPayload["id"]; got != "dm-1" {
			t.Fatalf("expected control message dm-1 to be forwarded, got %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for control message webhook submission")
	}

	// Give any (buggy) broadcast forwarding goroutines time to land, then
	// assert nothing else was delivered.
	time.Sleep(200 * time.Millisecond)
	select {
	case payload := <-delivered:
		t.Fatalf("broadcast message was forwarded to webhook: %+v", payload)
	default:
	}
}

func textEventForTest(eventID string, chat types.JID) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     chat,
				Sender:   types.NewJID("628111111111", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        eventID,
			Timestamp: time.Date(2026, time.May, 16, 8, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			Conversation: protoString("hello"),
		},
	}
}

type messageHandlerRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	mu                  sync.Mutex
	createMessageCalls  int
	createReactionCalls int
}

func (r *messageHandlerRepoSpy) CreateMessage(context.Context, *events.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createMessageCalls++
	return nil
}

func (r *messageHandlerRepoSpy) CreateReaction(context.Context, *events.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createReactionCalls++
	return nil
}

func (r *messageHandlerRepoSpy) createMessageCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createMessageCalls
}

func (r *messageHandlerRepoSpy) createReactionCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createReactionCalls
}

func reactionEventForTest(eventID, targetID, emoji string) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("628123456789", types.DefaultUserServer),
				Sender:   types.NewJID("628111111111", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        eventID,
			Timestamp: time.Date(2026, time.May, 16, 8, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ReactionMessage: &waE2E.ReactionMessage{
				Key: &waCommon.MessageKey{
					RemoteJID: protoString("628123456789@s.whatsapp.net"),
					FromMe:    protoBool(false),
					ID:        protoString(targetID),
				},
				Text: protoString(emoji),
			},
		},
	}
}
