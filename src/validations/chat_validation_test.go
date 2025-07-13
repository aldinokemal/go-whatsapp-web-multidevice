package validations

import (
	"context"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
)

func TestValidatePinChat(t *testing.T) {
	type args struct {
		request domainChat.PinChatRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid request",
			args: args{request: domainChat.PinChatRequest{
				ChatJID: "6289685028129@s.whatsapp.net",
				Pinned:  true,
			}},
			err: nil,
		},
		{
			name: "should success with unpin request",
			args: args{request: domainChat.PinChatRequest{
				ChatJID: "6289685028129@s.whatsapp.net",
				Pinned:  false,
			}},
			err: nil,
		},
		{
			name: "should error with empty chat_jid",
			args: args{request: domainChat.PinChatRequest{
				ChatJID: "",
				Pinned:  true,
			}},
			err: pkgError.ValidationError("chat_jid: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePinChat(context.Background(), &tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}
