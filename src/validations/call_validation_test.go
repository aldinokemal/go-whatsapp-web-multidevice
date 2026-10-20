package validations

import (
	"context"
	"testing"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/stretchr/testify/require"
)

func TestValidateStartCallRequiresTarget(t *testing.T) {
	tests := []struct {
		name    string
		request domainCall.StartCallRequest
		wantErr bool
	}{
		{name: "phone number", request: domainCall.StartCallRequest{Target: "+628123456789"}},
		{name: "jid with audio file", request: domainCall.StartCallRequest{
			Target:        "628123456789@s.whatsapp.net",
			AudioFilePath: "storages/hello.mp3",
		}},
		{name: "blank target", request: domainCall.StartCallRequest{Target: "   "}, wantErr: true},
		{name: "missing target", request: domainCall.StartCallRequest{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStartCall(context.Background(), tt.request)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestValidateCallIDTrimsAndRequiresValue(t *testing.T) {
	require.NoError(t, ValidateCallID(context.Background(), "  1ABCDEF  "))
	require.Error(t, ValidateCallID(context.Background(), ""))
	require.Error(t, ValidateCallID(context.Background(), "   "))
}
