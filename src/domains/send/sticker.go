package send

import "mime/multipart"

type StickerRequest struct {
	BaseRequest
	Sticker    *multipart.FileHeader `json:"sticker" form:"sticker"`
	StickerURL *string               `json:"sticker_url" form:"sticker_url"`
}