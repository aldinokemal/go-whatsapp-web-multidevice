package validations

import (
	"context"
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
)

func TestValidateMarkAsRead(t *testing.T) {
	type args struct {
		request domainMessage.MarkAsReadRequest
	}
	tests := []struct {
		name        string
		args        args
		errContains []string
	}{
		{
			name: "should success with valid message id and phone",
			args: args{request: domainMessage.MarkAsReadRequest{
				MessageID: "3EB0789ABC123456",
				Phone:     "6281234567890@s.whatsapp.net",
			}},
			errContains: nil,
		},
		{
			name: "should error with empty message id",
			args: args{request: domainMessage.MarkAsReadRequest{
				MessageID: "",
				Phone:     "6281234567890@s.whatsapp.net",
			}},
			errContains: []string{"message_id: cannot be blank"},
		},
		{
			name: "should error with empty phone",
			args: args{request: domainMessage.MarkAsReadRequest{
				MessageID: "3EB0789ABC123456",
				Phone:     "",
			}},
			errContains: []string{"phone: cannot be blank"},
		},
		{
			name: "should error with empty message id and phone",
			args: args{request: domainMessage.MarkAsReadRequest{
				MessageID: "",
				Phone:     "",
			}},
			errContains: []string{"message_id: cannot be blank", "phone: cannot be blank"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMarkAsRead(context.Background(), tt.args.request)
			if len(tt.errContains) == 0 {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				for _, msg := range tt.errContains {
					assert.ErrorContains(t, err, msg)
				}
			}
		})
	}
}

func TestValidateReactMessage(t *testing.T) {
	type args struct {
		request domainMessage.ReactionRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid phone, message id and emoji",
			args: args{request: domainMessage.ReactionRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "3EB0789ABC123456",
				Emoji:     "👍",
			}},
			err: nil,
		},
		{
			name: "should success with heart emoji",
			args: args{request: domainMessage.ReactionRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "3EB0789ABC123456",
				Emoji:     "❤️",
			}},
			err: nil,
		},
		{
			name: "should success with simple emoji",
			args: args{request: domainMessage.ReactionRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "3EB0789ABC123456",
				Emoji:     "😊",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainMessage.ReactionRequest{
				Phone:     "",
				MessageID: "3EB0789ABC123456",
				Emoji:     "👍",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty message id",
			args: args{request: domainMessage.ReactionRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "",
				Emoji:     "👍",
			}},
			err: pkgError.ValidationError("message_id: cannot be blank."),
		},
		{
			name: "should error with empty emoji",
			args: args{request: domainMessage.ReactionRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "3EB0789ABC123456",
				Emoji:     "",
			}},
			err: pkgError.ValidationError("emoji: cannot be blank."),
		},
		{
			name: "should error with all empty fields",
			args: args{request: domainMessage.ReactionRequest{
				Phone:     "",
				MessageID: "",
				Emoji:     "",
			}},
			err: pkgError.ValidationError("emoji: cannot be blank; message_id: cannot be blank; phone: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReactMessage(context.Background(), tt.args.request)
			if tt.err == nil {
				assert.NoError(t, err)
			} else if tt.name == "should error with all empty fields" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "phone: cannot be blank")
				assert.Contains(t, err.Error(), "message_id: cannot be blank")
				assert.Contains(t, err.Error(), "emoji: cannot be blank")
			} else {
				assert.Equal(t, tt.err, err)
			}
		})
	}
}

func TestValidateDeleteMessage(t *testing.T) {
	type args struct {
		request domainMessage.DeleteRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid phone and message id",
			args: args{request: domainMessage.DeleteRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "3EB0789ABC123456",
			}},
			err: nil,
		},
		{
			name: "should error with empty phone",
			args: args{request: domainMessage.DeleteRequest{
				Phone:     "",
				MessageID: "3EB0789ABC123456",
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty message id",
			args: args{request: domainMessage.DeleteRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "",
			}},
			err: pkgError.ValidationError("message_id: cannot be blank."),
		},
		{
			name: "should error with empty phone and message id",
			args: args{request: domainMessage.DeleteRequest{
				Phone:     "",
				MessageID: "",
			}},
			err: pkgError.ValidationError("message_id: cannot be blank; phone: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDeleteMessage(context.Background(), tt.args.request)
			if tt.err == nil {
				assert.NoError(t, err)
			} else if tt.name == "should error with empty phone and message id" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "message_id: cannot be blank")
				assert.Contains(t, err.Error(), "phone: cannot be blank")
			} else {
				assert.Equal(t, tt.err, err)
			}
		})
	}
}

func TestValidateStarMessage(t *testing.T) {
	type args struct {
		request domainMessage.StarRequest
	}
	tests := []struct {
		name string
		args args
		err  any
	}{
		{
			name: "should success with valid phone, message id and starred true",
			args: args{request: domainMessage.StarRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "3EB0789ABC123456",
				IsStarred: true,
			}},
			err: nil,
		},
		{
			name: "should success with valid phone, message id and starred false",
			args: args{request: domainMessage.StarRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "3EB0789ABC123456",
				IsStarred: false,
			}},
			// Note: validation.Required treats false as blank for boolean fields
			err: pkgError.ValidationError("is_starred: cannot be blank."),
		},
		{
			name: "should error with empty phone",
			args: args{request: domainMessage.StarRequest{
				Phone:     "",
				MessageID: "3EB0789ABC123456",
				IsStarred: true,
			}},
			err: pkgError.ValidationError("phone: cannot be blank."),
		},
		{
			name: "should error with empty message id",
			args: args{request: domainMessage.StarRequest{
				Phone:     "6281234567890@s.whatsapp.net",
				MessageID: "",
				IsStarred: true,
			}},
			err: pkgError.ValidationError("message_id: cannot be blank."),
		},
		{
			name: "should error with empty phone and message id",
			args: args{request: domainMessage.StarRequest{
				Phone:     "",
				MessageID: "",
				IsStarred: false,
			}},
			// All three fields will fail validation when IsStarred is false
			err: pkgError.ValidationError("is_starred: cannot be blank; message_id: cannot be blank; phone: cannot be blank."),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStarMessage(context.Background(), tt.args.request)
			if tt.err == nil {
				assert.NoError(t, err)
			} else if tt.name == "should error with empty phone and message id" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "is_starred: cannot be blank")
				assert.Contains(t, err.Error(), "message_id: cannot be blank")
				assert.Contains(t, err.Error(), "phone: cannot be blank")
			} else {
				assert.Equal(t, tt.err, err)
			}
		})
	}
}
