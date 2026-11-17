package usecase

import (
	"encoding/json"
	"testing"
	"time"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestGetChatMessagesExposesContextMetadata(t *testing.T) {
	for _, search := range []string{"", "context"} {
		name := "list"
		if search != "" {
			name = "search"
		}
		t.Run(name, func(t *testing.T) {
			_, repo, ctx := newMessageActionTestService(t, nil)
			const chatJID = "628123456789@s.whatsapp.net"
			const metadata = `{"replied_to_id":"original-a","future_field":true}`
			const referral = `{"ctwa_clid":"clid_123"}`
			const call = `{"outcome":"missed"}`
			now := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)

			require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
				ID: "message-1", ChatJID: chatJID, DeviceID: "device-a@s.whatsapp.net",
				Sender: chatJID, Content: "context with metadata", Timestamp: now,
				ContextMetadata: metadata, ReferralMetadata: referral, CallMetadata: call,
			}))
			require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
				ID: "message-plain", ChatJID: chatJID, DeviceID: "device-a@s.whatsapp.net",
				Sender: chatJID, Content: "context without metadata", Timestamp: now.Add(time.Minute),
			}))
			require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
				ID: "message-1", ChatJID: chatJID, DeviceID: "device-b@s.whatsapp.net",
				Sender: chatJID, Content: "context from another device", Timestamp: now,
				ContextMetadata: `{"replied_to_id":"other-device-secret"}`,
			}))

			response, err := NewChatService(repo).GetChatMessages(ctx, domainChat.GetChatMessagesRequest{
				ChatJID: chatJID, Limit: 50, Search: search,
			})
			require.NoError(t, err)
			require.Len(t, response.Data, 2)
			assert.Equal(t, 2, response.Pagination.Total)
			byID := make(map[string]domainChat.MessageInfo)
			for _, message := range response.Data {
				byID[message.ID] = message
			}
			require.Contains(t, byID, "message-1")
			require.Contains(t, byID, "message-plain")
			assert.Equal(t, metadata, byID["message-1"].ContextMetadata)
			assert.Equal(t, "context with metadata", byID["message-1"].Content)
			assert.Equal(t, referral, byID["message-1"].ReferralMetadata)
			assert.Equal(t, call, byID["message-1"].CallMetadata)
			assert.Empty(t, byID["message-plain"].ContextMetadata)

			payload, err := json.Marshal(response)
			require.NoError(t, err)
			var decoded struct {
				Data []map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(payload, &decoded))
			for _, message := range decoded.Data {
				if message["id"] == "message-1" {
					value, ok := message["context_metadata"].(string)
					require.True(t, ok, "context_metadata must be a JSON string, got %#v", message["context_metadata"])
					assert.Equal(t, metadata, value)
					assert.Equal(t, referral, message["referral_metadata"])
					assert.Equal(t, call, message["call_metadata"])
				} else {
					assert.NotContains(t, message, "context_metadata", "empty context metadata must be omitted")
				}
			}
		})
	}
}

func TestUpdateAndQuotePreserveDeviceScopedContextMetadata(t *testing.T) {
	service, repo, ctx := newMessageActionTestService(t, nil)
	const chatJID = "628123456789@s.whatsapp.net"
	const metadataA = `{"replied_to_id":"original-a","is_forwarded":true}`
	const metadataB = `{"replied_to_id":"original-b","mentioned_jid":["628111111111@s.whatsapp.net"]}`
	for deviceID, metadata := range map[string]string{
		"device-a@s.whatsapp.net": metadataA,
		"device-b@s.whatsapp.net": metadataB,
	} {
		message, err := repo.GetMessageByIDAndDevice(deviceID, "message-1")
		require.NoError(t, err)
		require.NotNil(t, message)
		message.Content = "original content from " + deviceID
		message.ContextMetadata = metadata
		require.NoError(t, repo.StoreMessage(message))
	}

	_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
		MessageID: "message-1", Phone: chatJID, Message: "edited selected-device content",
	})
	require.NoError(t, err)

	selected, err := repo.GetMessageByIDAndDevice("device-a@s.whatsapp.net", "message-1")
	require.NoError(t, err)
	require.NotNil(t, selected)
	assert.Equal(t, "edited selected-device content", selected.Content)
	assert.Equal(t, metadataA, selected.ContextMetadata)
	other, err := repo.GetMessageByIDAndDevice("device-b@s.whatsapp.net", "message-1")
	require.NoError(t, err)
	require.NotNil(t, other)
	assert.Equal(t, "original content from device-b@s.whatsapp.net", other.Content)
	assert.Equal(t, metadataB, other.ContextMetadata)

	replyID := "message-1"
	contextInfo := &waE2E.ContextInfo{Expiration: proto.Uint32(3600)}
	sender := serviceSend{chatStorageRepo: repo}
	quoted := sender.mergeReplyContext(ctx, contextInfo, &replyID)
	require.Same(t, contextInfo, quoted)
	assert.Equal(t, replyID, quoted.GetStanzaID())
	assert.Equal(t, "device-a@s.whatsapp.net", quoted.GetParticipant())
	assert.Equal(t, "edited selected-device content", quoted.GetQuotedMessage().GetConversation())
	assert.Equal(t, uint32(3600), quoted.GetExpiration())
}
