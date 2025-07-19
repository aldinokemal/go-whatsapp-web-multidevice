package validations

import (
	"context"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
)

func TestValidateListChats(t *testing.T) {
	type args struct {
		request domainChat.ListChatsRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid request",
			args: args{request: domainChat.ListChatsRequest{
				Limit:  25,
				Offset: 0,
			}},
			err: nil,
		},
		{
			name: "should success with zero limit (auto set to default)",
			args: args{request: domainChat.ListChatsRequest{
				Limit:  0,
				Offset: 0,
			}},
			err: nil,
		},
		{
			name: "should success with max limit",
			args: args{request: domainChat.ListChatsRequest{
				Limit:  100,
				Offset: 50,
			}},
			err: nil,
		},
		{
			name: "should error with limit too high",
			args: args{request: domainChat.ListChatsRequest{
				Limit:  101,
				Offset: 0,
			}},
			err: pkgError.ValidationError("limit: must be no greater than 100."),
		},
		{
			name: "should error with negative offset",
			args: args{request: domainChat.ListChatsRequest{
				Limit:  25,
				Offset: -1,
			}},
			err: pkgError.ValidationError("offset: must be no less than 0."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateListChats(context.Background(), &tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

func TestValidateGetChatMessages(t *testing.T) {
	type args struct {
		request domainChat.GetChatMessagesRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid request",
			args: args{request: domainChat.GetChatMessagesRequest{
				ChatJID: "6289685028129@s.whatsapp.net",
				Limit:   50,
				Offset:  0,
			}},
			err: nil,
		},
		{
			name: "should success with zero limit (auto set to default)",
			args: args{request: domainChat.GetChatMessagesRequest{
				ChatJID: "6289685028129@s.whatsapp.net",
				Limit:   0,
				Offset:  0,
			}},
			err: nil,
		},
		{
			name: "should success with max limit",
			args: args{request: domainChat.GetChatMessagesRequest{
				ChatJID: "6289685028129@s.whatsapp.net",
				Limit:   100,
				Offset:  50,
			}},
			err: nil,
		},
		{
			name: "should error with empty chat_jid",
			args: args{request: domainChat.GetChatMessagesRequest{
				ChatJID: "",
				Limit:   50,
				Offset:  0,
			}},
			err: pkgError.ValidationError("chat_jid: cannot be blank."),
		},
		{
			name: "should error with limit too high",
			args: args{request: domainChat.GetChatMessagesRequest{
				ChatJID: "6289685028129@s.whatsapp.net",
				Limit:   101,
				Offset:  0,
			}},
			err: pkgError.ValidationError("limit: must be no greater than 100."),
		},
		{
			name: "should error with negative offset",
			args: args{request: domainChat.GetChatMessagesRequest{
				ChatJID: "6289685028129@s.whatsapp.net",
				Limit:   50,
				Offset:  -1,
			}},
			err: pkgError.ValidationError("offset: must be no less than 0."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGetChatMessages(context.Background(), &tt.args.request)
			assert.Equal(t, tt.err, err)
		})
	}
}

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
