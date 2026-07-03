package send

import "mime/multipart"

type ImageRequest struct {
	BaseRequest
	Caption        string                `json:"caption" form:"caption"`
	ReplyMessageID *string               `json:"reply_message_id" form:"reply_message_id"`
	Image          *multipart.FileHeader `json:"image" form:"image"`
	ImageURL       *string               `json:"image_url" form:"image_url"`
	ViewOnce       bool                  `json:"view_once" form:"view_once"`
	Compress       bool                  `json:"compress"`
}
