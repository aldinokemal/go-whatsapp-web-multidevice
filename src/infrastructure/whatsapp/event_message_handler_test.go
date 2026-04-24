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
)

func TestHandleMessageReactionStoresReactionAndForwardsWebhook(t *testing.T) {
	originalWebhookURLs := config.WhatsappWebhook
	originalWebhookEvents := config.WhatsappWebhookEvents
	originalSubmit := submitWebhookFn
	defer func() {
		config.WhatsappWebhook = originalWebhookURLs
		config.WhatsappWebhookEvents = originalWebhookEvents
		submitWebhookFn = originalSubmit
	}()

	config.WhatsappWebhook = []string{"https://example.invalid/webhook"}
	config.WhatsappWebhookEvents = nil

	webhookCh := make(chan string, 1)
	submitWebhookFn = func(_ context.Context, payload map[string]any, _ string) error {
		event, _ := payload["event"].(string)
		webhookCh <- event
		return nil
	}

	repo := &messageHandlerRepoSpy{}
	device := NewDeviceInstance("device-1", nil, repo)
	ctx := ContextWithDevice(context.Background(), device)

	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("628123456789", types.DefaultUserServer),
				Sender:   types.NewJID("628999000111", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "reaction-event-1",
			Timestamp: time.Date(2026, time.April, 24, 10, 1, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ReactionMessage: &waE2E.ReactionMessage{
				Text: protoString("👍"),
				Key: &waCommon.MessageKey{
					ID: protoString("message-123"),
				},
			},
		},
	}

	handleMessage(ctx, evt, repo, nil)

	if repo.createReactionCount != 1 {
		t.Fatalf("expected reaction path to call CreateReaction once, got %d", repo.createReactionCount)
	}
	if repo.createMessageCount != 0 {
		t.Fatalf("expected reaction path not to call CreateMessage, got %d", repo.createMessageCount)
	}

	select {
	case event := <-webhookCh:
		if event != EventTypeMessageReaction {
			t.Fatalf("expected webhook event %q, got %q", EventTypeMessageReaction, event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected webhook forwarding for reaction event")
	}
}

type messageHandlerRepoSpy struct {
	mu                 sync.Mutex
	createMessageCount int
	createReactionCount int
}

func (r *messageHandlerRepoSpy) CreateMessage(context.Context, *events.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createMessageCount++
	return nil
}

func (r *messageHandlerRepoSpy) CreateReaction(context.Context, *events.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createReactionCount++
	return nil
}

func (r *messageHandlerRepoSpy) CreateIncomingCallRecord(context.Context, *events.CallOffer, bool) error {
	return nil
}

func (r *messageHandlerRepoSpy) StoreChat(*domainChatStorage.Chat) error { return nil }

func (r *messageHandlerRepoSpy) GetChat(string) (*domainChatStorage.Chat, error) { return nil, nil }

func (r *messageHandlerRepoSpy) GetChatByDevice(string, string) (*domainChatStorage.Chat, error) { return nil, nil }

func (r *messageHandlerRepoSpy) GetChats(*domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	return nil, nil
}

func (r *messageHandlerRepoSpy) DeleteChat(string) error { return nil }

func (r *messageHandlerRepoSpy) DeleteChatByDevice(string, string) error { return nil }

func (r *messageHandlerRepoSpy) StoreMessage(*domainChatStorage.Message) error { return nil }

func (r *messageHandlerRepoSpy) StoreMessagesBatch([]*domainChatStorage.Message) error { return nil }

func (r *messageHandlerRepoSpy) GetMessageByID(string) (*domainChatStorage.Message, error) { return nil, nil }

func (r *messageHandlerRepoSpy) GetMessages(*domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	return nil, nil
}

func (r *messageHandlerRepoSpy) SearchMessages(string, string, string, int) ([]*domainChatStorage.Message, error) {
	return nil, nil
}

func (r *messageHandlerRepoSpy) DeleteMessage(string, string) error { return nil }

func (r *messageHandlerRepoSpy) DeleteMessageByDevice(string, string, string) error { return nil }

func (r *messageHandlerRepoSpy) StoreSentMessageWithContext(context.Context, string, string, string, string, time.Time, *waE2E.Message) error {
	return nil
}

func (r *messageHandlerRepoSpy) GetChatMessageCount(string) (int64, error) { return 0, nil }

func (r *messageHandlerRepoSpy) GetChatMessageCountByDevice(string, string) (int64, error) { return 0, nil }

func (r *messageHandlerRepoSpy) GetTotalMessageCount() (int64, error) { return 0, nil }

func (r *messageHandlerRepoSpy) GetTotalChatCount() (int64, error) { return 0, nil }

func (r *messageHandlerRepoSpy) GetFilteredChatCount(*domainChatStorage.ChatFilter) (int64, error) { return 0, nil }

func (r *messageHandlerRepoSpy) GetChatNameWithPushName(types.JID, string, string, string) string { return "" }

func (r *messageHandlerRepoSpy) GetChatNameWithPushNameByDevice(string, types.JID, string, string, string) string {
	return ""
}

func (r *messageHandlerRepoSpy) GetStorageStatistics() (int64, int64, error) { return 0, 0, nil }

func (r *messageHandlerRepoSpy) TruncateAllChats() error { return nil }

func (r *messageHandlerRepoSpy) TruncateAllDataWithLogging(string) error { return nil }

func (r *messageHandlerRepoSpy) DeleteDeviceData(string) error { return nil }

func (r *messageHandlerRepoSpy) SaveDeviceRecord(*domainChatStorage.DeviceRecord) error { return nil }

func (r *messageHandlerRepoSpy) ListDeviceRecords() ([]*domainChatStorage.DeviceRecord, error) { return nil, nil }

func (r *messageHandlerRepoSpy) GetDeviceRecord(string) (*domainChatStorage.DeviceRecord, error) { return nil, nil }

func (r *messageHandlerRepoSpy) DeleteDeviceRecord(string) error { return nil }

func (r *messageHandlerRepoSpy) InitializeSchema() error { return nil }

var _ domainChatStorage.IChatStorageRepository = (*messageHandlerRepoSpy)(nil)
