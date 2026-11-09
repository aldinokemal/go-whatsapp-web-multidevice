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

func TestRevokeMessageUsesCurrentDeviceAndChat(t *testing.T) {
	groupJID := types.NewJID("120363000000000002", types.GroupServer)
	senderJID := types.NewJID("628987654321", types.DefaultUserServer)
	currentDevice := "device-a@s.whatsapp.net"

	tests := []struct {
		name        string
		otherDevice string
		otherChat   string
	}{
		{
			name:        "message is outgoing on another connected device",
			otherDevice: senderJID.String(),
			otherChat:   groupJID.String(),
		},
		{
			name:        "message ID also exists in another chat",
			otherDevice: currentDevice,
			otherChat:   "120363000000000001@g.us",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, repo, ctx := newMessageActionTestService(t, nil)
			for _, message := range []domainChatStorage.Message{
				{DeviceID: tt.otherDevice, ChatJID: tt.otherChat, Sender: senderJID.String(), IsFromMe: true},
				{DeviceID: currentDevice, ChatJID: groupJID.String(), Sender: senderJID.String()},
			} {
				message.ID = "revoke-target"
				message.Content = "message to revoke"
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
				revoke := message.GetProtocolMessage()
				require.NotNil(t, revoke)
				assert.Equal(t, waE2E.ProtocolMessage_REVOKE, revoke.GetType())
				assert.Equal(t, "revoke-target", revoke.GetKey().GetID())
				assert.Equal(t, groupJID.String(), revoke.GetKey().GetRemoteJID())
				assert.False(t, revoke.GetKey().GetFromMe(), "the current device is revoking another member's message")
				assert.Equal(t, senderJID.String(), revoke.GetKey().GetParticipant())
				return whatsmeow.SendResponse{ID: "revoke-1"}, nil
			}

			response, err := service.RevokeMessage(ctx, domainMessage.RevokeRequest{
				MessageID: "revoke-target",
				Phone:     groupJID.String(),
			})
			require.NoError(t, err)
			assert.True(t, sent)
			assert.Equal(t, "revoke-1", response.MessageID)

			message, err := repo.GetMessageByIDChatAndDevice(currentDevice, groupJID.String(), "revoke-target")
			require.NoError(t, err)
			assert.Nil(t, message)
			otherMessage, err := repo.GetMessageByIDChatAndDevice(tt.otherDevice, tt.otherChat, "revoke-target")
			require.NoError(t, err)
			require.NotNil(t, otherMessage, "local cleanup must preserve the other device/chat's copy")
		})
	}
}
