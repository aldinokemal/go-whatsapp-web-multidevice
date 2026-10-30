package validations

import (
	"context"
	"errors"
	"math"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func ValidateFollowNewsletter(ctx context.Context, request domainNewsletter.FollowRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.NewsletterID, validation.Required, validation.By(validateNewsletterJIDShape)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

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

func ValidateGetNewsletterInfo(ctx context.Context, request domainNewsletter.GetInfoRequest) error {
	hasID := strings.TrimSpace(request.NewsletterID) != ""
	hasInvite := strings.TrimSpace(request.Invite) != ""
	if hasID == hasInvite {
		return pkgError.ValidationError("exactly one of newsletter_id or invite must be provided.")
	}

	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.NewsletterID, validation.By(validateNewsletterJIDShape)),
	)
	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateGetNewsletterMessageUpdates(ctx context.Context, request *domainNewsletter.GetMessageUpdatesRequest) error {
	if request.Count == 0 {
		request.Count = 50
	}

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.NewsletterID, validation.Required, validation.By(validateNewsletterJIDShape)),
		validation.Field(&request.Count, validation.Min(1), validation.Max(100)),
		validation.Field(&request.Since, validation.Min(int64(0))),
		validation.Field(&request.After, validation.Required.Error("must be a message server ID; WhatsApp does not answer message_updates without it"), validation.Min(1)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateNewsletterLiveUpdates(ctx context.Context, request domainNewsletter.LiveUpdatesRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.NewsletterID, validation.Required, validation.By(validateNewsletterJIDShape)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateDownloadNewsletterMedia(ctx context.Context, request domainNewsletter.DownloadMediaRequest) error {
	err := validation.ValidateStructWithContext(ctx, &request,
		validation.Field(&request.NewsletterID, validation.Required, validation.By(validateNewsletterJIDShape)),
		validation.Field(&request.ServerID, validation.By(validatePositiveServerID)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func validatePositiveServerID(value any) error {
	serverID, ok := value.(int)
	if !ok || serverID < 1 {
		return errors.New("must be no less than 1")
	}
	if serverID == math.MaxInt {
		return errors.New("must be less than the maximum integer value")
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

var newsletterDirectoryViews = []any{"RECOMMENDED", "TRENDING", "POPULAR", "NEW", "FEATURED"}

var newsletterDirectoryCategories = []any{"BUSINESS", "ENTERTAINMENT", "LIFESTYLE", "NEWS", "ORGANIZATIONS", "PEOPLE", "SPORTS"}

func ValidateNewsletterDirectory(ctx context.Context, request *domainNewsletter.DirectoryRequest) error {
	request.View = strings.ToUpper(strings.TrimSpace(request.View))
	request.SearchText = strings.TrimSpace(request.SearchText)
	if request.View == "" {
		request.View = "RECOMMENDED"
	}
	if request.Limit == 0 {
		request.Limit = 20
	}
	request.CountryCodes = normalizeUpperList(request.CountryCodes)
	request.Categories = normalizeUpperList(request.Categories)

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.View, validation.In(newsletterDirectoryViews...)),
		validation.Field(&request.Limit, validation.Min(1), validation.Max(50)),
		validation.Field(&request.CountryCodes,
			validation.When(request.SearchText == "", validation.Required.Error("at least one country code is required to browse the directory")),
			validation.Each(validation.Length(2, 2))),
		validation.Field(&request.Categories, validation.Each(validation.In(newsletterDirectoryCategories...))),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

// normalizeUpperList accepts both repeated query params and a single
// comma-separated value, trimming and upper-casing every entry.
func normalizeUpperList(values []string) []string {
	var result []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.ToUpper(strings.TrimSpace(part)); part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}
