package send

type MessageRequest struct {
	Phone   string `json:"phone" form:"phone"`
	Message string `json:"message" form:"message"`
}

type MessageResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
