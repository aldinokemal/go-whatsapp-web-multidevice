package validations

import (
	"context"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func ValidateUnfollowNewsletter(ctx context.Context, request domainNewsletter.UnfollowRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.NewsletterID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateGetNewsletterMessages(ctx context.Context, request *domainNewsletter.GetMessagesRequest) error {
	// Set default count if not provided
	if request.Count == 0 {
		request.Count = 50
	}

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.NewsletterID, validation.Required, validation.By(validateNewsletterJIDShape)),
		validation.Field(&request.Count, validation.Min(1), validation.Max(100)),
		validation.Field(&request.Before, validation.Min(0)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

// validateNewsletterJIDShape rejects newsletter IDs that are not shaped like a
// newsletter JID (e.g. a phone/group JID), so mistargeted requests fail fast
// with a clear validation error instead of a WhatsApp protocol round-trip.
func validateNewsletterJIDShape(value any) error {
	id, ok := value.(string)
	if !ok || id == "" {
		// Presence is already enforced by validation.Required; skip format
		// checking on empty/non-string values here.
		return nil
	}
	if !strings.HasSuffix(id, config.WhatsappTypeNewsletter) {
		return pkgError.ValidationError("must end with " + config.WhatsappTypeNewsletter)
	}
	return nil
}
