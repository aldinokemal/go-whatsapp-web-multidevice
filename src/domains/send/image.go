package send

import "mime/multipart"

type ImageRequest struct {
	Phone    string                `json:"phone" form:"phone"`
	Caption  string                `json:"caption" form:"caption"`
	Image    *multipart.FileHeader `json:"image" form:"image"`
	ViewOnce bool                  `json:"view_once" form:"view_once"`
	Type     Type                  `json:"type" form:"type"`
	Compress bool                  `json:"compress"`
}

type ImageResponse struct {
	Status string `json:"status"`
}
