package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func TestProcessConversationMessagesPersistsReactionEvents(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	repo := &historyReactionRepoSpy{}

	ctx := ContextWithDevice(context.Background(), NewDeviceInstance(deviceID, nil, nil))
	syncType := waHistorySync.HistorySync_RECENT
	reactionTimestamp := uint64(time.Date(2026, time.May, 16, 8, 2, 0, 0, time.UTC).Unix())
	data := &waHistorySync.HistorySync{
		SyncType: &syncType,
		Conversations: []*waHistorySync.Conversation{
			{
				ID: proto.String(chatJID),
				Messages: []*waHistorySync.HistorySyncMsg{
					{
						Message: &waWeb.WebMessageInfo{
							Key: &waCommon.MessageKey{
								RemoteJID: proto.String(chatJID),
								FromMe:    proto.Bool(false),
								ID:        proto.String("reaction-event-1"),
							},
							Message: &waE2E.Message{
								ReactionMessage: &waE2E.ReactionMessage{
									Key: &waCommon.MessageKey{
										RemoteJID: proto.String(chatJID),
										FromMe:    proto.Bool(false),
										ID:        proto.String("msg-1"),
									},
									Text: proto.String("\U0001f44d"),
								},
							},
							MessageTimestamp: &reactionTimestamp,
						},
					},
				},
			},
		},
	}

	if err := processConversationMessages(ctx, data, repo, nil); err != nil {
		t.Fatalf("process conversation messages: %v", err)
	}

	if repo.createReactionCalls != 1 {
		t.Fatalf("expected history reaction event to be persisted once, got %d", repo.createReactionCalls)
	}
	if repo.lastReaction == nil {
		t.Fatal("expected reaction event to be passed to repository")
	}
	if got := repo.lastReaction.Message.GetReactionMessage().GetText(); got != "\U0001f44d" {
		t.Fatalf("expected thumbs-up reaction, got %q", got)
	}
	if got := repo.lastReaction.Message.GetReactionMessage().GetKey().GetID(); got != "msg-1" {
		t.Fatalf("expected target message id msg-1, got %q", got)
	}
}

func TestProcessConversationMessagesPersistsPollDefinitionWithoutText(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	repo := &historyPollRepoSpy{}
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance(deviceID, nil, nil))
	syncType := waHistorySync.HistorySync_RECENT
	timestamp := uint64(time.Date(2026, time.August, 26, 8, 2, 0, 0, time.UTC).Unix())
	data := &waHistorySync.HistorySync{
		SyncType: &syncType,
		Conversations: []*waHistorySync.Conversation{{
			ID: proto.String(chatJID),
			Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{
				Key: &waCommon.MessageKey{RemoteJID: proto.String(chatJID), ID: proto.String("POLL-HISTORY-1")},
				Message: &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{
					Name: proto.String("History poll"),
					Options: []*waE2E.PollCreationMessage_Option{
						{OptionName: proto.String("One")},
						{OptionName: proto.String("Two")},
					},
				}},
				MessageTimestamp: &timestamp,
			}}},
		}},
	}

	if err := processConversationMessages(ctx, data, repo, nil); err != nil {
		t.Fatalf("processConversationMessages: %v", err)
	}
	if repo.definition == nil || repo.definition.DeviceID != deviceID || repo.definition.PollMessageID != "POLL-HISTORY-1" {
		t.Fatalf("poll definition not persisted: %+v", repo.definition)
	}
	if repo.definition.Question != "History poll" || len(repo.definition.Options) != 2 || repo.definition.Version != "v3" {
		t.Fatalf("unexpected poll definition: %+v", repo.definition)
	}
	wantTimestamp := time.Unix(int64(timestamp), 0)
	assert.True(t, repo.definition.UpdatedAt.Equal(wantTimestamp),
		"poll definition updated_at = %s, want history timestamp %s",
		repo.definition.UpdatedAt, wantTimestamp)
}

// TestProcessHistorySyncRoutesOnDemandToConversationMessages pins the
// on-demand history sync routing fix: HISTORY_SYNC_ON_DEMAND (the phone's
// reply to Client.BuildHistorySyncRequest, used for "load older messages")
// must be persisted the same way as INITIAL_BOOTSTRAP/RECENT, not silently
// dropped by the sync-type switch in processHistorySync.
func TestProcessHistorySyncRoutesOnDemandToConversationMessages(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	repo := &historyMessageBatchRepoSpy{}
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance(deviceID, nil, nil))
	syncType := waHistorySync.HistorySync_ON_DEMAND
	timestamp := uint64(time.Date(2026, time.September, 6, 8, 0, 0, 0, time.UTC).Unix())
	data := &waHistorySync.HistorySync{
		SyncType: &syncType,
		Conversations: []*waHistorySync.Conversation{{
			ID: proto.String(chatJID),
			Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{
				Key: &waCommon.MessageKey{
					RemoteJID: proto.String(chatJID),
					FromMe:    proto.Bool(false),
					ID:        proto.String("older-msg-1"),
				},
				Message:          &waE2E.Message{Conversation: proto.String("an older message")},
				MessageTimestamp: &timestamp,
			}}},
		}},
	}

	if err := processHistorySync(ctx, data, repo, nil); err != nil {
		t.Fatalf("processHistorySync: %v", err)
	}

	if repo.storeMessagesBatchCalls != 1 {
		t.Fatalf("expected on-demand conversations to be persisted once, got %d calls", repo.storeMessagesBatchCalls)
	}
	if len(repo.lastBatch) != 1 || repo.lastBatch[0].ID != "older-msg-1" {
		t.Fatalf("unexpected persisted batch: %+v", repo.lastBatch)
	}
}

// TestProcessConversationMessagesOnDemandPreservesNewerChatMetadata pins the
// on-demand chat-metadata regression: HISTORY_SYNC_ON_DEMAND carries messages
// older than the local anchor, so its batch timestamp must never move an
// already-newer LastMessageTime backward, unarchive an archived chat, nor
// clear the disappearing-messages timer the chunk does not report.
func TestProcessConversationMessagesOnDemandPreservesNewerChatMetadata(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	existingLastMessageTime := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	repo := &historyMessageBatchRepoSpy{
		existingChat: &domainChatStorage.Chat{
			DeviceID:            deviceID,
			JID:                 chatJID,
			LastMessageTime:     existingLastMessageTime,
			Archived:            true,
			EphemeralExpiration: 604800,
		},
	}
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance(deviceID, nil, nil))
	syncType := waHistorySync.HistorySync_ON_DEMAND
	olderTimestamp := uint64(time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC).Unix())
	data := &waHistorySync.HistorySync{
		SyncType: &syncType,
		Conversations: []*waHistorySync.Conversation{{
			ID: proto.String(chatJID),
			Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{
				Key: &waCommon.MessageKey{
					RemoteJID: proto.String(chatJID),
					FromMe:    proto.Bool(false),
					ID:        proto.String("older-msg-2"),
				},
				Message:          &waE2E.Message{Conversation: proto.String("an even older message")},
				MessageTimestamp: &olderTimestamp,
			}}},
		}},
	}

	if err := processConversationMessages(ctx, data, repo, nil); err != nil {
		t.Fatalf("processConversationMessages: %v", err)
	}

	if repo.lastStoredChat == nil {
		t.Fatal("expected chat to be stored")
	}
	if !repo.lastStoredChat.LastMessageTime.Equal(existingLastMessageTime) {
		t.Fatalf("expected LastMessageTime to stay at %s, got %s", existingLastMessageTime, repo.lastStoredChat.LastMessageTime)
	}
	if !repo.lastStoredChat.Archived {
		t.Fatal("expected Archived to remain true after on-demand sync")
	}
	if repo.lastStoredChat.EphemeralExpiration != 604800 {
		t.Fatalf("expected EphemeralExpiration to stay at 604800, got %d", repo.lastStoredChat.EphemeralExpiration)
	}
}

// History sync delivers messages still wrapped (e.g. in disappearing-message
// chats). A wrapped captioned image must keep its media columns, or re-syncing
// a row the live path stored would wipe them, and a wrapped business template
// must not be skipped.
func TestProcessConversationMessagesUnwrapsHistoryMessages(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	repo := &historyMessageBatchRepoSpy{}
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance(deviceID, nil, nil))
	syncType := waHistorySync.HistorySync_ON_DEMAND
	timestamp := uint64(time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC).Unix())
	ephemeral := func(id string, inner *waE2E.Message) *waHistorySync.HistorySyncMsg {
		return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
			Key:              &waCommon.MessageKey{RemoteJID: proto.String(chatJID), FromMe: proto.Bool(false), ID: proto.String(id)},
			Message:          &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: inner}},
			MessageTimestamp: &timestamp,
		}}
	}
	data := &waHistorySync.HistorySync{
		SyncType: &syncType,
		Conversations: []*waHistorySync.Conversation{{
			ID: proto.String(chatJID),
			Messages: []*waHistorySync.HistorySyncMsg{
				ephemeral("img-1", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
					Caption:    proto.String("look at this"),
					DirectPath: proto.String("/v/t62/abc"),
					MediaKey:   []byte{1, 2, 3},
				}}),
				ephemeral("tpl-1", &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
					HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
						Title:               &waE2E.TemplateMessage_HydratedFourRowTemplate_HydratedTitleText{HydratedTitleText: "Order confirmed"},
						HydratedContentText: proto.String("Your order #42 has shipped"),
					},
				}}),
			},
		}},
	}

	if err := processConversationMessages(ctx, data, repo, nil); err != nil {
		t.Fatalf("processConversationMessages: %v", err)
	}

	if len(repo.lastBatch) != 2 {
		t.Fatalf("expected both wrapped messages to be stored, got %d", len(repo.lastBatch))
	}
	image, template := repo.lastBatch[0], repo.lastBatch[1]
	if image.MediaType != "image" || image.DirectPath != "/v/t62/abc" || len(image.MediaKey) == 0 || image.Content != "look at this" {
		t.Fatalf("wrapped image lost its media: %+v", image)
	}
	if template.Content != "Order confirmed\nYour order #42 has shipped" {
		t.Fatalf("unexpected template content %q", template.Content)
	}
	// On main this chat had no storable message, so it was never created.
	if repo.lastStoredChat == nil || repo.lastStoredChat.JID != chatJID {
		t.Fatalf("expected the chat to be stored, got %+v", repo.lastStoredChat)
	}
}

type historyMessageBatchRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	storeMessagesBatchCalls int
	lastBatch               []*domainChatStorage.Message
	existingChat            *domainChatStorage.Chat
	lastStoredChat          *domainChatStorage.Chat
}

func (r *historyMessageBatchRepoSpy) StoreChat(chat *domainChatStorage.Chat) error {
	r.lastStoredChat = chat
	return nil
}

func (r *historyMessageBatchRepoSpy) GetChatByDevice(_, _ string) (*domainChatStorage.Chat, error) {
	return r.existingChat, nil
}

func (r *historyMessageBatchRepoSpy) StoreMessagesBatch(messages []*domainChatStorage.Message) error {
	r.storeMessagesBatchCalls++
	r.lastBatch = messages
	return nil
}

func (r *historyMessageBatchRepoSpy) GetChatNameWithPushName(jid types.JID, _ string, _ string, pushName string) string {
	if pushName != "" {
		return pushName
	}
	return jid.String()
}

type historyReactionRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	createReactionCalls int
	lastReaction        *events.Message
}

type historyPollRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	definition *domainChatStorage.PollDefinition
}

func (r *historyPollRepoSpy) UpsertPollDefinition(definition *domainChatStorage.PollDefinition) error {
	r.definition = definition
	return nil
}

func (r *historyPollRepoSpy) GetChatNameWithPushName(jid types.JID, _ string, _ string, pushName string) string {
	if pushName != "" {
		return pushName
	}
	return jid.String()
}

func (r *historyReactionRepoSpy) CreateReaction(_ context.Context, evt *events.Message) error {
	r.createReactionCalls++
	r.lastReaction = evt
	return nil
}

func (r *historyReactionRepoSpy) GetChatNameWithPushName(jid types.JID, _ string, _ string, pushName string) string {
	if pushName != "" {
		return pushName
	}
	return jid.String()
}
