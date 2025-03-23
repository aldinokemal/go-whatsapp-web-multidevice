package send

import "mime/multipart"

type VideoRequest struct {
	Phone       string                `json:"phone" form:"phone"`
	Caption     string                `json:"caption" form:"caption"`
	Video       *multipart.FileHeader `json:"video" form:"video"`
	ViewOnce    bool                  `json:"view_once" form:"view_once"`
	Compress    bool                  `json:"compress"`
	IsForwarded bool                  `json:"is_forwarded" form:"is_forwarded"`
}
