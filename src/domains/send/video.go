package send

import "mime/multipart"

type VideoRequest struct {
	BaseRequest
	Caption        string                `json:"caption" form:"caption"`
	ReplyMessageID *string               `json:"reply_message_id" form:"reply_message_id"`
	Video          *multipart.FileHeader `json:"video" form:"video"`
	ViewOnce       bool                  `json:"view_once" form:"view_once"`
	Compress       bool                  `json:"compress"`
	GifPlayback    bool                  `json:"gif_playback" form:"gif_playback"`
	VideoURL       *string               `json:"video_url" form:"video_url"`
}
