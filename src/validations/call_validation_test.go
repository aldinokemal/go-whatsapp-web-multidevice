package validations

import (
	"testing"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/assert"
)

func TestValidateListCallLogs(t *testing.T) {
	tests := []struct {
		name      string
		request   domainCall.ListCallLogsRequest
		wantLimit int
		wantErr   error
	}{
		{"valid", domainCall.ListCallLogsRequest{Limit: 25}, 25, nil},
		{"default limit", domainCall.ListCallLogsRequest{}, 25, nil},
		{"max limit with chat filter", domainCall.ListCallLogsRequest{Limit: 100, Offset: 50, ChatJID: "6289685028129@s.whatsapp.net"}, 100, nil},
		{"limit too high", domainCall.ListCallLogsRequest{Limit: 101}, 101, pkgError.ValidationError("limit: must be no greater than 100.")},
		{"negative limit", domainCall.ListCallLogsRequest{Limit: -1}, -1, pkgError.ValidationError("limit: must be no less than 1.")},
		{"negative offset", domainCall.ListCallLogsRequest{Limit: 25, Offset: -1}, 25, pkgError.ValidationError("offset: must be no less than 0.")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateListCallLogs(t.Context(), &tt.request)
			assert.Equal(t, tt.wantErr, err)
			assert.Equal(t, tt.wantLimit, tt.request.Limit)
		})
	}
}
