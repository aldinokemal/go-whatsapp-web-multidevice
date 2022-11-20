package send

type MessageRequest struct {
	Phone   string `json:"phone" form:"phone"`
	Message string `json:"message" form:"message"`
	Type    Type   `json:"type" form:"type"`
}

type MessageResponse struct {
	Status string `json:"status"`
}
