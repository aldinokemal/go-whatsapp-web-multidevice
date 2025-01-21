package send

import "mime/multipart"

type VideoRequest struct {
	Phone    string                `json:"phone" form:"phone"`
	Caption  string                `json:"caption" form:"caption"`
	Video    *multipart.FileHeader `json:"-" form:"video"`   // Ignore in JSON
	ViewOnce bool                  `json:"view_once" form:"view_once"`
	Compress bool                  `json:"compress"`
	VideoUrl string                `json:"video_url" form:"video_url"`
}
