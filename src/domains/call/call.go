package call

import "context"

type ICallUsecase interface {
	RejectCall(ctx context.Context, callerJID string, callID string) error
	ListCallLogs(ctx context.Context, request ListCallLogsRequest) (ListCallLogsResponse, error)
}

type RejectCallRequest struct {
	CallerJID string `json:"caller_jid" form:"caller_jid"`
	CallID    string `json:"call_id" form:"call_id"`
}

type ListCallLogsRequest struct {
	Limit   int    `json:"limit" query:"limit"`
	Offset  int    `json:"offset" query:"offset"`
	ChatJID string `json:"chat_jid" query:"chat_jid"`
}

type ListCallLogsResponse struct {
	Data       []CallLogInfo      `json:"data"`
	Pagination PaginationResponse `json:"pagination"`
}

type CallLogInfo struct {
	ID           string       `json:"id"`
	ChatJID      string       `json:"chat_jid"`
	SenderJID    string       `json:"sender_jid"`
	Timestamp    string       `json:"timestamp"`
	IsFromMe     bool         `json:"is_from_me"`
	CallMetadata CallMetadata `json:"call_metadata"`
}

// CallMetadata mirrors the JSON stored on the synthetic call message row.
type CallMetadata struct {
	CallID         string `json:"call_id"`
	AutoRejected   bool   `json:"auto_rejected"`
	RemotePlatform string `json:"remote_platform,omitempty"`
	RemoteVersion  string `json:"remote_version,omitempty"`
	GroupJID       string `json:"group_jid,omitempty"`
}

type PaginationResponse struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}
