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
	SendLocation(ctx context.Context, request LocationRequest) (response LocationResponse, err error)
}
