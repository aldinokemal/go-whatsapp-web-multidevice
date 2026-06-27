package send

import "mime/multipart"

type AudioRequest struct {
	BaseRequest
	Audio          *multipart.FileHeader `json:"audio" form:"audio"`
	AudioURL       *string               `json:"audio_url" form:"audio_url"`
	ReplyMessageID *string               `json:"reply_message_id" form:"reply_message_id"`
	PTT            bool                  `json:"ptt" form:"ptt"`
}
