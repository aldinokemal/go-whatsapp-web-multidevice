package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
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

func TestProcessConversationMessagesPersistsReplyContext(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	deviceID := "device-a@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"
	repo := &historySyncReplyRepoSpy{}

	ctx := ContextWithDevice(context.Background(), NewDeviceInstance(deviceID, nil, nil))
	syncType := waHistorySync.HistorySync_RECENT
	replyText := "Thanks for the update"
	quotedCaption := "Launch checklist"
	messageTimestamp := uint64(time.Date(2026, time.May, 16, 8, 5, 0, 0, time.UTC).Unix())
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
								ID:        proto.String("reply-event-1"),
							},
							Message: &waE2E.Message{
								ExtendedTextMessage: &waE2E.ExtendedTextMessage{
									Text: proto.String(replyText),
									ContextInfo: &waE2E.ContextInfo{
										StanzaID: proto.String("quoted-message-1"),
										QuotedMessage: &waE2E.Message{
											ImageMessage: &waE2E.ImageMessage{
												Caption: proto.String(quotedCaption),
											},
										},
									},
								},
							},
							MessageTimestamp: &messageTimestamp,
						},
					},
				},
			},
		},
	}

	if err := processConversationMessages(ctx, data, repo, nil); err != nil {
		t.Fatalf("process conversation messages: %v", err)
	}

	if repo.storeMessagesBatchCalls != 1 {
		t.Fatalf("expected StoreMessagesBatch to be called once, got %d", repo.storeMessagesBatchCalls)
	}
	if len(repo.lastMessagesBatch) != 1 {
		t.Fatalf("expected one stored message in batch, got %d", len(repo.lastMessagesBatch))
	}

	stored := repo.lastMessagesBatch[0]
	if stored.RepliedToID != "quoted-message-1" {
		t.Fatalf("expected replied_to_id quoted-message-1, got %q", stored.RepliedToID)
	}
	if stored.QuotedBody != quotedCaption {
		t.Fatalf("expected quoted_body %q, got %q", quotedCaption, stored.QuotedBody)
	}
}

type historyReactionRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	createReactionCalls int
	lastReaction        *events.Message
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

type historySyncReplyRepoSpy struct {
	domainChatStorage.IChatStorageRepository
	storeChatCalls          int
	storeMessagesBatchCalls int
	lastMessagesBatch       []*domainChatStorage.Message
}

func (r *historySyncReplyRepoSpy) StoreChat(_ *domainChatStorage.Chat) error {
	r.storeChatCalls++
	return nil
}

func (r *historySyncReplyRepoSpy) StoreMessagesBatch(messages []*domainChatStorage.Message) error {
	r.storeMessagesBatchCalls++
	r.lastMessagesBatch = messages
	return nil
}

func (r *historySyncReplyRepoSpy) GetChatNameWithPushName(jid types.JID, _ string, _ string, pushName string) string {
	if pushName != "" {
		return pushName
	}
	return jid.String()
}
