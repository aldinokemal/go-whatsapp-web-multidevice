package send

import "mime/multipart"

type AudioRequest struct {
	Phone string                `json:"phone" form:"phone"`
	Audio *multipart.FileHeader `json:"audio" form:"audio"`
}
