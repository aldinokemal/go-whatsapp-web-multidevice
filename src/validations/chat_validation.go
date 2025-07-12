package validations

import (
	"context"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

func ValidateListChats(ctx context.Context, request *domainChat.ListChatsRequest) error {
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

func ValidateGetChatMessages(ctx context.Context, request *domainChat.GetChatMessagesRequest) error {
	// Set default limit if not provided
	if request.Limit == 0 {
		request.Limit = 50
	}

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
		validation.Field(&request.Limit, validation.Min(1), validation.Max(100)),
		validation.Field(&request.Offset, validation.Min(0)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidatePinChat(ctx context.Context, request *domainChat.PinChatRequest) error {
	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}
