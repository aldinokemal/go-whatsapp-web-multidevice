package send

import "mime/multipart"

type VideoRequest struct {
	BaseRequest
	Caption  string                `json:"caption" form:"caption"`
	Video    *multipart.FileHeader `json:"video" form:"video"`
	ViewOnce bool                  `json:"view_once" form:"view_once"`
	Compress bool                  `json:"compress"`
	VideoURL *string               `json:"video_url" form:"video_url"`
}
