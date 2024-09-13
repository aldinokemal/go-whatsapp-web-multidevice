package message

import "context"

type IMessageService interface {
	ReactMessage(ctx context.Context, request ReactionRequest) (response GenericResponse, err error)
	RevokeMessage(ctx context.Context, request RevokeRequest) (response GenericResponse, err error)
	UpdateMessage(ctx context.Context, request UpdateMessageRequest) (response GenericResponse, err error)
	DeleteMessage(ctx context.Context, request DeleteRequest) (err error)
}

type GenericResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

type RevokeRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type DeleteRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type ReactionRequest struct {
	MessageID string `json:"message_id" form:"message_id"`
	Phone     string `json:"phone" form:"phone"`
	Emoji     string `json:"emoji" form:"emoji"`
}

type UpdateMessageRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Message   string `json:"message" form:"message"`
	Phone     string `json:"phone" form:"phone"`
}
