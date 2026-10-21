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

// ValidateStartCall checks the beta outbound-call request. The targets accepted
// here mirror the rest of the API: a phone number or a JID.
func ValidateStartCall(ctx context.Context, request domainCall.StartCallRequest) error {
	request.Target = strings.TrimSpace(request.Target)
	request.AudioFilePath = strings.TrimSpace(request.AudioFilePath)

	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Target, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

// ValidateCallID checks the beta call-id-only requests (answer, hangup).
func ValidateCallID(ctx context.Context, callID string) error {
	request := struct {
		CallID string
	}{CallID: strings.TrimSpace(callID)}

	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.CallID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}
