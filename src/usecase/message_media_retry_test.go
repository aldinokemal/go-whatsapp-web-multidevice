package usecase

import (
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

func TestMediaRetryMessageInfoDirectChat(t *testing.T) {
	info, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:      "3A0000000000000000AA",
		ChatJID: "5511888888888@s.whatsapp.net",
		Sender:  "5511888888888@s.whatsapp.net",
	})

	require.NoError(t, err)
	require.Equal(t, types.MessageID("3A0000000000000000AA"), info.ID)
	require.Equal(t, types.NewJID("5511888888888", types.DefaultUserServer), info.Chat)
	require.False(t, info.IsGroup)
	require.False(t, info.IsFromMe)
	require.True(t, info.Sender.IsEmpty(), "direct chats do not send a participant")
}

func TestMediaRetryMessageInfoGroupCarriesSender(t *testing.T) {
	info, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:       "3A0000000000000000BB",
		ChatJID:  "120363000000000001@g.us",
		Sender:   "123456789012345:7@lid",
		IsFromMe: true,
	})

	require.NoError(t, err)
	require.True(t, info.IsGroup)
	require.True(t, info.IsFromMe)
	require.Equal(t, types.NewJID("123456789012345", types.HiddenUserServer), info.Sender)
}

func TestMediaRetryMessageInfoGroupWithoutSenderFails(t *testing.T) {
	_, err := mediaRetryMessageInfo(&domainChatStorage.Message{
		ID:      "3A0000000000000000CC",
		ChatJID: "120363000000000001@g.us",
	})

	require.ErrorContains(t, err, "no valid sender")
}

func TestMediaRetryMessageInfoInvalidChatFails(t *testing.T) {
	_, err := mediaRetryMessageInfo(&domainChatStorage.Message{ID: "X", ChatJID: ""})

	require.ErrorContains(t, err, "invalid chat JID")
}
