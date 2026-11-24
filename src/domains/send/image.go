package send

import "mime/multipart"

type ImageRequest struct {
	BaseRequest
	Caption        string                `json:"caption" form:"caption"`
	ReplyMessageID *string               `json:"reply_message_id" form:"reply_message_id"`
	Mentions       []string              `json:"mentions,omitempty" form:"mentions"` // Phone numbers/JIDs to mention (ghost mentions)
	Image          *multipart.FileHeader `json:"image" form:"image"`
	ImageURL       *string               `json:"image_url" form:"image_url"`
	// ImageBytes carries already-decoded image data straight from the MCP tool,
	// which has no multipart upload and no URL to fetch, only base64 in the
	// call's own arguments. Never populated from JSON or a form; set only by
	// the MCP handler after it decodes image_base64.
	ImageBytes []byte `json:"-" form:"-"`
	// ImageFilename names ImageBytes on disk (an extension is all that
	// matters); the MCP handler defaults it when the caller leaves it out.
	ImageFilename string `json:"-" form:"-"`
	ViewOnce      bool   `json:"view_once" form:"view_once"`
	Compress      bool   `json:"compress"`
	HD            bool   `json:"hd" form:"hd"`
	AllowReshare  bool   `json:"allow_reshare,omitempty" form:"allow_reshare"` // status@broadcast only: let viewers reshare the status
}
