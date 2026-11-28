package validations

import (
	"context"
	"strings"

	domainLabel "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/label"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// ValidateEditLabel checks a label create, rename or delete request. It trims
// the identifiers and upper-cases the type.
func ValidateEditLabel(ctx context.Context, request *domainLabel.EditLabelRequest) error {
	request.LabelID = strings.TrimSpace(request.LabelID)
	request.Name = strings.TrimSpace(request.Name)
	request.Type = strings.ToUpper(strings.TrimSpace(request.Type))

	nameRules := []validation.Rule{validation.Length(0, 255)}
	if !request.Deleted {
		nameRules = append(nameRules, validation.Required)
	}

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.LabelID, validation.Required, validation.Length(1, 64)),
		validation.Field(&request.Name, nameRules...),
		validation.Field(&request.Color, validation.Min(int32(0))),
		validation.Field(&request.OrderIndex, validation.Min(int32(0))),
		validation.Field(&request.Type, validation.In("", domainLabel.LabelTypeCustom).Error(`must be empty or "CUSTOM"`)),
	)
	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}

// ValidateLabelChat checks a request that adds a chat to a label or removes it.
func ValidateLabelChat(ctx context.Context, request *domainLabel.LabelChatRequest) error {
	request.LabelID = strings.TrimSpace(request.LabelID)

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
		validation.Field(&request.LabelID, validation.Required, validation.Length(1, 64)),
		validation.Field(&request.Labeled, validation.NotNil),
	)
	if err != nil {
		return pkgError.ValidationError(err.Error())
	}
	return nil
}
