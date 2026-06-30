package validations

import (
	"context"
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
		validation.Field(&request.NewsletterID, validation.Required),
		validation.Field(&request.Count, validation.Min(1), validation.Max(100)),
		validation.Field(&request.Before, validation.Min(0)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}
