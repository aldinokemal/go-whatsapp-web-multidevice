package message

import "context"

type IMessageService interface {
	ReactMessage(ctx context.Context, request ReactionRequest) (response ReactionResponse, err error)
	RevokeMessage(ctx context.Context, request RevokeRequest) (response RevokeResponse, err error)
	UpdateMessage(ctx context.Context, request UpdateMessageRequest) (response UpdateMessageResponse, err error)
}
