package send

import "mime/multipart"

type ImageRequest struct {
	Phone    string                `json:"phone" form:"phone"`
	Caption  string                `json:"caption" form:"caption"`
	Image    *multipart.FileHeader `json:"image" form:"image"`
	ViewOnce bool                  `json:"view_once" form:"view_once"`
	Compress bool                  `json:"compress"`
}

type ImageResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
