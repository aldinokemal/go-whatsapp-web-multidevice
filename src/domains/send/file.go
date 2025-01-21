package send

import "mime/multipart"

type FileRequest struct {
	Phone   string                `json:"phone" form:"phone"`
	File    *multipart.FileHeader `json:"-" form:"file"`    // Ignore in JSON
	Caption string                `json:"caption" form:"caption"`
	FileUrl string                `json:"file_url" form:"file_url"`
}
