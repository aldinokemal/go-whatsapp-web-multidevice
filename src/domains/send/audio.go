package send

import "mime/multipart"

type AudioRequest struct {
	BaseRequest
	Audio    *multipart.FileHeader `json:"audio" form:"audio"`
	AudioURL *string               `json:"audio_url" form:"audio_url"`
}
