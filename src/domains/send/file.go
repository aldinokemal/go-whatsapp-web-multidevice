package send

import "mime/multipart"

type FileRequest struct {
	Phone string                `json:"phone" form:"phone"`
	File  *multipart.FileHeader `json:"file" form:"file"`
	Type  Type                  `json:"type" form:"type"`
}

type FileResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
