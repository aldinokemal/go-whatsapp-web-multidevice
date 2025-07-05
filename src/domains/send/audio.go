package send

import "mime/multipart"

type AudioRequest struct {
	Phone       string                `json:"phone" form:"phone"`
	Audio       *multipart.FileHeader `json:"audio" form:"audio"`
	AudioURL    *string               `json:"audio_url" form:"audio_url"`
	IsForwarded bool                  `json:"is_forwarded" form:"is_forwarded"`
}
