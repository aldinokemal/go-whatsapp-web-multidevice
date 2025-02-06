package send

type MessageRequest struct {
	Phone   string               `json:"phone" form:"phone"`
	Message string               `json:"message" form:"message"`
	Reply   *ReplyMessageRequest `json:"reply" form:"reply"`
}

type ReplyMessageRequest struct {
	ReplyMessageID string `json:"reply_message_id" form:"reply_message_id"`
	ParticipantJID string `json:"participant_jid" form:"participant_jid"`
	Quote          string `json:"quote" form:"quote"`
}
