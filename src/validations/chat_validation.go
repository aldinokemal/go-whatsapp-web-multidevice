package validations

import (
	"context"
	"strings"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"go.mau.fi/whatsmeow/types"
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

// ValidTimerValues contains WhatsApp's allowed disappearing message durations in seconds
var ValidTimerValues = []uint32{
	0,       // Disabled
	86400,   // 24 hours
	604800,  // 7 days
	7776000, // 90 days
}

func ValidateSetDisappearingTimer(ctx context.Context, request *domainChat.SetDisappearingTimerRequest) error {
	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
		validation.Field(&request.TimerSeconds, validation.By(validateTimerValue)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func validateTimerValue(value any) error {
	timer, ok := value.(uint32)
	if !ok {
		return pkgError.ValidationError("timer_seconds must be a valid number")
	}
	for _, valid := range ValidTimerValues {
		if timer == valid {
			return nil
		}
	}
	return pkgError.ValidationError("timer_seconds must be one of: 0 (off), 86400 (24h), 604800 (7d), 7776000 (90d)")
}

// MaxRequestChatHistoryCount caps how many older messages can be requested
// from the phone in a single on-demand history sync request.
const MaxRequestChatHistoryCount = 500

func ValidateRequestChatHistory(ctx context.Context, request *domainChat.RequestChatHistoryRequest) error {
	// Set default count if not provided
	if request.Count == 0 {
		request.Count = 50
	}

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
		validation.Field(&request.Count, validation.Min(1), validation.Max(MaxRequestChatHistoryCount)),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateArchiveChat(ctx context.Context, request *domainChat.ArchiveChatRequest) error {
	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}

func ValidateDeleteChat(ctx context.Context, request *domainChat.DeleteChatRequest) error {
	if err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.ChatJID, validation.Required),
	); err != nil {
		return pkgError.ValidationError(err.Error())
	}
	jid, err := types.ParseJID(request.ChatJID)
	if err != nil || strings.Count(request.ChatJID, "@") != 1 || jid.User == "" || strings.ContainsAny(jid.User, "/\\\x00 \t\r\n") || jid.User == "." || jid.User == ".." {
		return pkgError.InvalidJID("chat_jid must be a valid chat JID")
	}
	switch jid.Server {
	case types.DefaultUserServer, types.HiddenUserServer, types.GroupServer, types.BroadcastServer, types.NewsletterServer:
	default:
		return pkgError.InvalidJID("unsupported chat_jid server")
	}
	return nil
}
