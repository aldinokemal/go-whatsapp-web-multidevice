package whatsapp

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func TestProcessHistorySyncPreservesContextMetadata(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })

	chatJID := "628123456789@s.whatsapp.net"
	accountJID := types.NewADJID("628111111111", 0, 7)
	deviceClient := &whatsmeow.Client{Store: &store.Device{ID: &accountJID}}
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance("device-alias", deviceClient, nil))
	// The event's device must win over an unrelated client passed to history sync.
	otherAccountJID := types.NewADJID("628222222222", 0, 9)
	otherClient := &whatsmeow.Client{Store: &store.Device{ID: &otherAccountJID}}

	reply := func(parentID string) *waE2E.Message {
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("historical reply"),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(parentID),
				Participant:   proto.String(chatJID),
				QuotedMessage: &waE2E.Message{Conversation: proto.String("quoted message")},
			},
		}}
	}
	cases := []struct {
		name            string
		message         *waE2E.Message
		contextMetadata string
		content         string
		mediaType       string
	}{
		{
			name:            "direct reply",
			message:         reply("direct-parent"),
			contextMetadata: `{"replied_to_id":"direct-parent"}`,
			content:         "historical reply",
		},
		{
			name: "ephemeral reply",
			message: &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{
				Message: reply("ephemeral-parent"),
			}},
			contextMetadata: `{"replied_to_id":"ephemeral-parent"}`,
			content:         "historical reply",
		},
		{
			name: "nested view once image reply",
			message: &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{
					Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
						Caption:     proto.String("historical image reply"),
						ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("image-parent")},
					}},
				}},
			}},
			contextMetadata: `{"replied_to_id":"image-parent"}`,
			content:         "historical image reply",
			mediaType:       "image",
		},
		{
			name:    "no context",
			message: &waE2E.Message{Conversation: proto.String("ordinary historical message")},
			content: "ordinary historical message",
		},
		{
			name: "context without reply",
			message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String("forwarded historical message"),
				ContextInfo: &waE2E.ContextInfo{IsForwarded: proto.Bool(true)},
			}},
			content: "forwarded historical message",
		},
	}

	for _, syncType := range []waHistorySync.HistorySync_HistorySyncType{
		waHistorySync.HistorySync_INITIAL_BOOTSTRAP,
		waHistorySync.HistorySync_RECENT,
		waHistorySync.HistorySync_ON_DEMAND,
	} {
		t.Run(syncType.String(), func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					repo := &historyMessageBatchRepoSpy{}
					data := historySyncWithContextMessage(syncType, chatJID, tc.message)
					if err := processHistorySync(ctx, data, repo, otherClient); err != nil {
						t.Fatalf("processHistorySync: %v", err)
					}
					if repo.storeMessagesBatchCalls != 1 || len(repo.lastBatch) != 1 {
						t.Fatalf("expected one history message to be stored, got %d calls and %d messages", repo.storeMessagesBatchCalls, len(repo.lastBatch))
					}
					message := repo.lastBatch[0]
					if message.ContextMetadata != tc.contextMetadata {
						t.Errorf("ContextMetadata = %q, want %q", message.ContextMetadata, tc.contextMetadata)
					}
					if message.Content != tc.content || message.MediaType != tc.mediaType {
						t.Errorf("history content/media = %q/%q, want %q/%q", message.Content, message.MediaType, tc.content, tc.mediaType)
					}
					if message.DeviceID != accountJID.ToNonAD().String() || message.ChatJID != chatJID {
						t.Errorf("message scope = %q/%q, want %q/%q", message.DeviceID, message.ChatJID, accountJID.ToNonAD().String(), chatJID)
					}
					if repo.lastStoredChat == nil || repo.lastStoredChat.DeviceID != message.DeviceID || repo.lastStoredChat.JID != chatJID {
						t.Errorf("chat did not retain the message's device/chat scope: %+v", repo.lastStoredChat)
					}
				})
			}
		})
	}
}

func TestProcessHistorySyncContextMetadataUsesClientDeviceFallback(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })

	accountJID := types.NewADJID("628111111111", 0, 7)
	client := &whatsmeow.Client{Store: &store.Device{ID: &accountJID}}
	repo := &historyMessageBatchRepoSpy{}
	data := historySyncWithContextMessage(waHistorySync.HistorySync_RECENT, "628123456789@s.whatsapp.net", &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String("reply"),
			ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("parent-id")},
		},
	})
	if err := processHistorySync(context.Background(), data, repo, client); err != nil {
		t.Fatalf("processHistorySync: %v", err)
	}
	if len(repo.lastBatch) != 1 {
		t.Fatalf("expected one history message to be stored, got %d", len(repo.lastBatch))
	}
	message := repo.lastBatch[0]
	if message.DeviceID != accountJID.ToNonAD().String() {
		t.Errorf("DeviceID = %q, want %q", message.DeviceID, accountJID.ToNonAD().String())
	}
	if message.ContextMetadata != `{"replied_to_id":"parent-id"}` {
		t.Errorf("unexpected reply context: %q", message.ContextMetadata)
	}
}

func historySyncWithContextMessage(syncType waHistorySync.HistorySync_HistorySyncType, chatJID string, message *waE2E.Message) *waHistorySync.HistorySync {
	return &waHistorySync.HistorySync{
		SyncType: &syncType,
		Conversations: []*waHistorySync.Conversation{{
			ID: proto.String(chatJID),
			Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{
				Key: &waCommon.MessageKey{
					RemoteJID: proto.String(chatJID),
					FromMe:    proto.Bool(false),
					ID:        proto.String("historical-message"),
				},
				Message:          message,
				MessageTimestamp: proto.Uint64(1788249600),
			}}},
		}},
	}
}
