package validations

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestValidateSendMessage(t *testing.T) {
	type args struct {
		request structs.SendMessageRequest
	}
	tests := []struct {
		name string
		args args
		err  interface{}
	}{
		{
			name: "success phone & message normal",
			args: args{request: structs.SendMessageRequest{
				Phone:   "6289685024091",
				Message: "Hello this is testing",
			}},
			err: nil,
		},
		{
			name: "error invalid phone",
			args: args{request: structs.SendMessageRequest{
				Phone:   "some-random-phone",
				Message: "Hello this is testing",
			}},
			err: utils.ValidationError{
				Message: "phone: must contain digits only.",
			},
		},
		{
			name: "error invalid phone contains dash (-)",
			args: args{request: structs.SendMessageRequest{
				Phone:   "6289-748-291",
				Message: "Hello this is testing",
			}},
			err: utils.ValidationError{
				Message: "phone: must contain digits only.",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				ValidateSendMessage(tt.args.request)
			} else {
				assert.PanicsWithValue(t, tt.err, func() {
					ValidateSendMessage(tt.args.request)
				})
			}

		})
	}
}
