package send

type MessageRequest struct {
	BaseRequest
	Message        string   `json:"message" form:"message"`
	ReplyMessageID *string  `json:"reply_message_id" form:"reply_message_id"`
	Mentions       []string `json:"mentions,omitempty" form:"mentions"` // List of phone numbers/JIDs to mention (ghost mentions)
}
