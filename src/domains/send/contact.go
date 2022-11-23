package send

type ContactRequest struct {
	Phone        string `json:"phone" form:"phone"`
	ContactName  string `json:"contact_name" form:"contact_name"`
	ContactPhone string `json:"contact_phone" form:"contact_phone"`
}

type ContactResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
