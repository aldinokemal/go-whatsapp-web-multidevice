package send

import "mime/multipart"

type ImageRequest struct {
	Phone    string                `json:"phone" form:"phone"`
	Image    *multipart.FileHeader `json:"-" form:"image"`    // Ignore in JSON
	Caption  string                `json:"caption" form:"caption"`
	ImageUrl string                `json:"image_url" form:"image_url"`
	ViewOnce bool                  `json:"view_once" form:"view_once"`
	Compress bool                  `json:"compress"`
}
