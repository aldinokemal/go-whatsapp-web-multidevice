package send

import "mime/multipart"

type VideoRequest struct {
	Phone    string                `json:"phone" form:"phone"`
	Caption  string                `json:"caption" form:"caption"`
	Video    *multipart.FileHeader `json:"video" form:"video"`
	ViewOnce bool                  `json:"view_once" form:"view_once"`
	Compress bool                  `json:"compress"`
}

type VideoResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
