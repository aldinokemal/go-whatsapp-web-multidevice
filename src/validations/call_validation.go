package validations

import (
	"context"
	"strings"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func ValidateRejectCall(ctx context.Context, callerJID string, callID string) error {
	callerJID = strings.TrimSpace(callerJID)
	callID = strings.TrimSpace(callID)
	request := domainCall.RejectCallRequest{
		CallerJID: callerJID,
		CallID:    callID,
	}
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.CallerJID, validation.Required),
		validation.Field(&request.CallID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateListCallLogs(ctx context.Context, request *domainCall.ListCallLogsRequest) error {
	// Set default limit if not provided
	if request.Limit == 0 {
		request.Limit = 25
	}

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.Limit, validation.Min(1), validation.Max(100)),
		validation.Field(&request.Offset, validation.Min(0)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}
