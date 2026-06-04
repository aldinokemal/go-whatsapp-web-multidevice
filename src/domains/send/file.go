package send

import "mime/multipart"

type FileRequest struct {
	BaseRequest
	File           *multipart.FileHeader `json:"file" form:"file"`
	FileURL        *string               `json:"file_url" form:"file_url"`
	Caption        string                `json:"caption" form:"caption"`
	ReplyMessageID *string               `json:"reply_message_id" form:"reply_message_id"`
}
