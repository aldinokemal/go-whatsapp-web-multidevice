package send

import "mime/multipart"

type FileRequest struct {
	BaseRequest
	File     *multipart.FileHeader `json:"file" form:"file"`
	Mimetype *string               `json:"mimetype" form:"mimetype"`
	FileURL  *string               `json:"file_url" form:"file_url"`
	Caption  string                `json:"caption" form:"caption"`
}
