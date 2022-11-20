package send

import "mime/multipart"

type FileRequest struct {
	Phone string                `json:"phone" form:"phone"`
	File  *multipart.FileHeader `json:"file" form:"file"`
	Type  Type                  `json:"type" form:"type"`
}

type FileResponse struct {
	Status string `json:"status"`
}
