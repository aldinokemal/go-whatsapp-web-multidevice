package send

type LocationRequest struct {
	Phone     string `json:"phone" form:"phone"`
	Latitude  string `json:"latitude" form:"latitude"`
	Longitude string `json:"longitude" form:"longitude"`
}

type LocationResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
