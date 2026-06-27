package validations

import (
	"context"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func ValidateStartCall(ctx context.Context, request domainCall.StartCallRequest) error {
	if err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.Phone, validation.Required),
	); err != nil {
		return pkgError.ValidationError(err.Error())
	}
	if err := validatePhoneNumber(request.Phone); err != nil {
		return err
	}
	if request.Video {
		return pkgError.ValidationError("video calls are not supported")
	}
	return nil
}

func ValidateCallIDRequest(ctx context.Context, request domainCall.CallIDRequest) error {
	if err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.CallID, validation.Required),
	); err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}

func ValidateWebRTCRequest(ctx context.Context, request domainCall.WebRTCRequest) error {
	if err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.CallID, validation.Required),
		validation.Field(&request.SDPOffer, validation.Required),
	); err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}
