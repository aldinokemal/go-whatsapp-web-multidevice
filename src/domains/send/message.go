package send

type RevokeRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type RevokeResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

type UpdateMessageRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Message   string `json:"message" form:"message"`
	Phone     string `json:"phone" form:"phone"`
}

type UpdateMessageResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
