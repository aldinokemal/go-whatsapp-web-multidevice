package validations

import (
	"context"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	"github.com/stretchr/testify/require"
)

func TestValidateDeleteChat(t *testing.T) {
	for _, jid := range []string{"628111@s.whatsapp.net", "999111@lid", "120-123@g.us", "status@broadcast", "120123@newsletter"} {
		require.NoError(t, ValidateDeleteChat(context.Background(), &domainChat.DeleteChatRequest{ChatJID: jid}))
	}
	for _, jid := range []string{"", "628111", "@g.us", "../123@g.us", "123/456@s.whatsapp.net", "..@lid", "123@unknown", "123@s.whatsapp.net@other", "123 @s.whatsapp.net", "123\x00@s.whatsapp.net"} {
		require.Error(t, ValidateDeleteChat(context.Background(), &domainChat.DeleteChatRequest{ChatJID: jid}), jid)
	}
}
