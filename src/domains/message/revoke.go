package message

type RevokeRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type RevokeResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
