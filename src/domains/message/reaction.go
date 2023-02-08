package message

type ReactionRequest struct {
	MessageID string `json:"message_id" form:"message_id"`
	Phone     string `json:"phone" form:"phone"`
	Emoji     string `json:"emoji" form:"emoji"`
}

type ReactionResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
