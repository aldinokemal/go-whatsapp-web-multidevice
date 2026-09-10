package validations

import (
	"context"
	"testing"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
)

func TestValidateListCallLogs(t *testing.T) {
	type args struct {
		request domainCall.ListCallLogsRequest
	}
	tests := []struct {
		name          string
		args          args
		err           any
		expectedLimit int
	}{
		{
			name:          "should success with valid request",
			args:          args{request: domainCall.ListCallLogsRequest{Limit: 25, Offset: 0}},
			err:           nil,
			expectedLimit: 25,
		},
		{
			name:          "should success with zero limit (auto set to default)",
			args:          args{request: domainCall.ListCallLogsRequest{Limit: 0, Offset: 0}},
			err:           nil,
			expectedLimit: 25,
		},
		{
			name:          "should success with max limit and chat filter",
			args:          args{request: domainCall.ListCallLogsRequest{Limit: 100, Offset: 50, ChatJID: "6289685028129@s.whatsapp.net"}},
			err:           nil,
			expectedLimit: 100,
		},
		{
			name:          "should error with limit too high",
			args:          args{request: domainCall.ListCallLogsRequest{Limit: 101, Offset: 0}},
			err:           pkgError.ValidationError("limit: must be no greater than 100."),
			expectedLimit: 101,
		},
		{
			name:          "should error with negative limit",
			args:          args{request: domainCall.ListCallLogsRequest{Limit: -1, Offset: 0}},
			err:           pkgError.ValidationError("limit: must be no less than 1."),
			expectedLimit: -1,
		},
		{
			name:          "should error with negative offset",
			args:          args{request: domainCall.ListCallLogsRequest{Limit: 25, Offset: -1}},
			err:           pkgError.ValidationError("offset: must be no less than 0."),
			expectedLimit: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateListCallLogs(context.Background(), &tt.args.request)
			assert.Equal(t, tt.err, err)
			assert.Equal(t, tt.expectedLimit, tt.args.request.Limit)
		})
	}
}
