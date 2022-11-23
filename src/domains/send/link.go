package send

type LinkRequest struct {
	Phone   string `json:"phone" form:"phone"`
	Caption string `json:"caption"`
	Link    string `json:"link"`
}

type LinkResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
