package validations

import (
	"context"
	"fmt"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
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
