package send

import (
	"context"
)

type ISendService interface {
	SendText(ctx context.Context, request MessageRequest) (response MessageResponse, err error)
	SendImage(ctx context.Context, request ImageRequest) (response ImageResponse, err error)
	SendFile(ctx context.Context, request FileRequest) (response FileResponse, err error)
	SendVideo(ctx context.Context, request VideoRequest) (response VideoResponse, err error)
	SendContact(ctx context.Context, request ContactRequest) (response ContactResponse, err error)
	SendLink(ctx context.Context, request LinkRequest) (response LinkResponse, err error)
	Revoke(ctx context.Context, request RevokeRequest) (response RevokeResponse, err error)
	UpdateMessage(ctx context.Context, request UpdateMessageRequest) (response UpdateMessageResponse, err error)
}
