package send

import (
	"context"
)

type ISendService interface {
	SendText(ctx context.Context, request MessageRequest) (response GenericResponse, err error)
	SendImage(ctx context.Context, request ImageRequest) (response GenericResponse, err error)
	SendFile(ctx context.Context, request FileRequest) (response GenericResponse, err error)
	SendVideo(ctx context.Context, request VideoRequest) (response GenericResponse, err error)
	SendContact(ctx context.Context, request ContactRequest) (response GenericResponse, err error)
	SendLink(ctx context.Context, request LinkRequest) (response GenericResponse, err error)
	SendLocation(ctx context.Context, request LocationRequest) (response GenericResponse, err error)
	SendAudio(ctx context.Context, request AudioRequest) (response GenericResponse, err error)
	SendPoll(ctx context.Context, request PollRequest) (response GenericResponse, err error)
	SendPresence(ctx context.Context, request PresenceRequest) (response GenericResponse, err error)
}

type GenericResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
