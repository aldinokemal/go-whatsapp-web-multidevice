package send

type LinkRequest struct {
	Phone   string `json:"phone" form:"phone"`
	Caption string `json:"caption"`
	Link    string `json:"link"`
	Type    Type   `json:"type" form:"type"`
}

type LinkResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
