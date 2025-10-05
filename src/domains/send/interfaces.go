package send

import (
	"context"
)

// ITextSender handles text message sending operations
type ITextSender interface {
	SendText(ctx context.Context, request MessageRequest) (response GenericResponse, err error)
}

// IMediaSender handles media message sending operations
type IMediaSender interface {
	SendImage(ctx context.Context, request ImageRequest) (response GenericResponse, err error)
	SendFile(ctx context.Context, request FileRequest) (response GenericResponse, err error)
	SendVideo(ctx context.Context, request VideoRequest) (response GenericResponse, err error)
	SendAudio(ctx context.Context, request AudioRequest) (response GenericResponse, err error)
	SendSticker(ctx context.Context, request StickerRequest) (response GenericResponse, err error)
}

// IInteractionSender handles interaction message sending operations
type IInteractionSender interface {
	SendContact(ctx context.Context, request ContactRequest) (response GenericResponse, err error)
	SendLink(ctx context.Context, request LinkRequest) (response GenericResponse, err error)
	SendLocation(ctx context.Context, request LocationRequest) (response GenericResponse, err error)
	SendPoll(ctx context.Context, request PollRequest) (response GenericResponse, err error)
}

// IPresenceSender handles presence-related operations
type IPresenceSender interface {
	SendPresence(ctx context.Context, request PresenceRequest) (response GenericResponse, err error)
	SendChatPresence(ctx context.Context, request ChatPresenceRequest) (response GenericResponse, err error)
}

// ISendUsecase combines all sender interfaces for backward compatibility
type ISendUsecase interface {
	ITextSender
	IMediaSender
	IInteractionSender
	IPresenceSender
}
