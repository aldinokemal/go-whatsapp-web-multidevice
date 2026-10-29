package usecase

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

func TestReactMessageUsesCurrentDeviceAndChat(t *testing.T) {
	groupJID := types.NewJID("120363000000000002", types.GroupServer)
	otherGroupJID := types.NewJID("120363000000000001", types.GroupServer)
	senderJID := types.NewJID("628987654321", types.DefaultUserServer)
	currentDevice := "device-a@s.whatsapp.net"

	tests := []struct {
		name            string
		messages        []domainChatStorage.Message
		wantFromMe      bool
		wantParticipant string
	}{
		{
			name: "incoming message also stored as outgoing on another device",
			messages: []domainChatStorage.Message{
				{DeviceID: senderJID.String(), ChatJID: groupJID.String(), Sender: senderJID.String(), IsFromMe: true},
				{DeviceID: currentDevice, ChatJID: groupJID.String(), Sender: senderJID.String()},
			},
			wantParticipant: senderJID.String(),
		},
		{
			name: "outgoing message also stored as incoming on another device",
			messages: []domainChatStorage.Message{
				{DeviceID: senderJID.String(), ChatJID: groupJID.String(), Sender: currentDevice},
				{DeviceID: currentDevice, ChatJID: groupJID.String(), Sender: currentDevice, IsFromMe: true},
			},
			wantFromMe: true,
		},
		{
			name: "same message ID in another chat",
			messages: []domainChatStorage.Message{
				{DeviceID: currentDevice, ChatJID: otherGroupJID.String(), Sender: currentDevice, IsFromMe: true},
				{DeviceID: currentDevice, ChatJID: groupJID.String(), Sender: senderJID.String()},
			},
			wantParticipant: senderJID.String(),
		},
		{
			name: "message only stored on another device keeps missing-message fallback",
			messages: []domainChatStorage.Message{
				{DeviceID: senderJID.String(), ChatJID: groupJID.String(), Sender: senderJID.String()},
			},
			wantFromMe: true,
		},
		{
			name: "message only stored in another chat keeps missing-message fallback",
			messages: []domainChatStorage.Message{
				{DeviceID: currentDevice, ChatJID: otherGroupJID.String(), Sender: senderJID.String()},
			},
			wantFromMe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, repo, ctx := newMessageActionTestService(t, nil)
			for _, message := range tt.messages {
				message.ID = "reaction-target"
				message.Content = "message to react to"
				message.Timestamp = time.Now()
				require.NoError(t, repo.StoreMessage(&message))
			}
			service.validateJIDFn = func(_ *whatsmeow.Client, _ string) (types.JID, error) {
				return groupJID, nil
			}
			sent := false
			service.sendMessageFn = func(_ context.Context, _ *whatsmeow.Client, recipient types.JID, message *waE2E.Message) (whatsmeow.SendResponse, error) {
				sent = true
				assert.Equal(t, groupJID, recipient)
				reaction := message.GetReactionMessage()
				require.NotNil(t, reaction)
				assert.Equal(t, "👍", reaction.GetText())
				assert.Equal(t, "reaction-target", reaction.GetKey().GetID())
				assert.Equal(t, groupJID.String(), reaction.GetKey().GetRemoteJID())
				assert.Equal(t, tt.wantFromMe, reaction.GetKey().GetFromMe())
				assert.Equal(t, tt.wantParticipant, reaction.GetKey().GetParticipant())
				return whatsmeow.SendResponse{ID: "reaction-1"}, nil
			}

			response, err := service.ReactMessage(ctx, domainMessage.ReactionRequest{
				MessageID: "reaction-target",
				Phone:     groupJID.String(),
				Emoji:     "👍",
			})
			require.NoError(t, err)
			assert.True(t, sent)
			assert.Equal(t, "reaction-1", response.MessageID)
		})
	}
}
