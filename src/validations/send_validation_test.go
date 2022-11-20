package validations

import (
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestValidateSendMessage(t *testing.T) {
	type args struct {
		request domainSend.MessageRequest
	}
	tests := []struct {
		name string
		args args
		err  interface{}
	}{
		{
			name: "success phone & message normal",
			args: args{request: domainSend.MessageRequest{
				Phone:   "6289685024091",
				Message: "Hello this is testing",
			}},
			err: nil,
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
