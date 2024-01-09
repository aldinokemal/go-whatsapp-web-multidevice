package send

import "mime/multipart"

type FileRequest struct {
	Phone   string                `json:"phone" form:"phone"`
	File    *multipart.FileHeader `json:"file" form:"file"`
	Caption string                `json:"caption" form:"caption"`
}
