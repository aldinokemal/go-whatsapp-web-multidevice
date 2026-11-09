package send

import "mime/multipart"

type StickerRequest struct {
	BaseRequest
	ReplyMessageID *string               `json:"reply_message_id" form:"reply_message_id"`
	Sticker        *multipart.FileHeader `json:"sticker" form:"sticker"`
	StickerURL     *string               `json:"sticker_url" form:"sticker_url"`
}
