package send

type RevokeRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
	Type      Type   `json:"type" form:"type"`
}

type RevokeResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
