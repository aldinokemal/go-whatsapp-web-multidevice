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
// already-newer LastMessageTime backward, nor unarchive an archived chat.
func TestProcessConversationMessagesOnDemandPreservesNewerChatMetadata(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	existingLastMessageTime := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	repo := &historyMessageBatchRepoSpy{
		existingChat: &domainChatStorage.Chat{
			DeviceID:        deviceID,
			JID:             chatJID,
			LastMessageTime: existingLastMessageTime,
			Archived:        true,
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
