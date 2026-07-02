package validations

import (
	"context"
	"fmt"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"go.mau.fi/whatsmeow/types"
	"regexp"
)

func ValidateLoginWithCode(ctx context.Context, phoneNumber string) error {
	// Combine validations using a single ValidateWithContext call
	err := validation.ValidateWithContext(ctx, &phoneNumber,
		validation.Required,
		validation.Match(regexp.MustCompile(`^\+?[0-9]{1,15}$`)),
	)
	if err != nil {
		return pkgError.ValidationError(fmt.Sprintf("phone_number(%s): %s", phoneNumber, err.Error()))
	}
	return nil
}

func ValidatePasskeyResponse(ctx context.Context, response *types.WebAuthnResponse) error {
	if response == nil {
		return pkgError.ValidationError("assertion payload is required")
	}

	err := validation.ValidateStructWithContext(ctx, response,
		validation.Field(&response.ID, validation.Required),
		validation.Field(&response.Type, validation.Required, validation.In("public-key")),
	)
	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	if len(response.RawID) == 0 {
		return pkgError.ValidationError("rawId: cannot be blank")
	}
	if len(response.Response.ClientDataJSON) == 0 {
		return pkgError.ValidationError("response.clientDataJSON: cannot be blank")
	}
	if len(response.Response.AuthenticatorData) == 0 {
		return pkgError.ValidationError("response.authenticatorData: cannot be blank")
	}
	if len(response.Response.Signature) == 0 {
		return pkgError.ValidationError("response.signature: cannot be blank")
	}
	return nil
}
