package validations

import (
	"context"
	"testing"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/stretchr/testify/assert"
)

func TestValidateStartCall(t *testing.T) {
	tests := []struct {
		name    string
		request domainCall.StartCallRequest
		wantErr bool
	}{
		{
			name: "accepts an international phone number",
			request: domainCall.StartCallRequest{
				Phone: "5511999999999",
			},
			wantErr: false,
		},
		{
			name:    "requires phone",
			request: domainCall.StartCallRequest{},
			wantErr: true,
		},
		{
			name: "rejects local phone numbers",
			request: domainCall.StartCallRequest{
				Phone: "011999999999",
			},
			wantErr: true,
		},
		{
			name: "rejects video calls for v1",
			request: domainCall.StartCallRequest{
				Phone: "5511999999999",
				Video: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStartCall(context.Background(), tt.request)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateCallIDRequest(t *testing.T) {
	assert.NoError(t, ValidateCallIDRequest(context.Background(), domainCall.CallIDRequest{CallID: "abc123"}))
	assert.Error(t, ValidateCallIDRequest(context.Background(), domainCall.CallIDRequest{}))
}

func TestValidateWebRTCRequest(t *testing.T) {
	assert.NoError(t, ValidateWebRTCRequest(context.Background(), domainCall.WebRTCRequest{
		CallID:   "abc123",
		SDPOffer: "v=0\r\n",
	}))
	assert.Error(t, ValidateWebRTCRequest(context.Background(), domainCall.WebRTCRequest{CallID: "abc123"}))
	assert.Error(t, ValidateWebRTCRequest(context.Background(), domainCall.WebRTCRequest{SDPOffer: "v=0\r\n"}))
}
