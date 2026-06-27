package call

import "context"

type ICallUsecase interface {
	RejectCall(ctx context.Context, callerJID string, callID string) error
}

type RejectCallRequest struct {
	CallerJID string `json:"caller_jid" form:"caller_jid"`
	CallID    string `json:"call_id" form:"call_id"`
}
