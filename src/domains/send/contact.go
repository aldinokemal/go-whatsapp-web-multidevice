package send

type ContactRequest struct {
	Phone        string `json:"phone" form:"phone"`
	ContactName  string `json:"contact_name" form:"contact_name"`
	ContactPhone string `json:"contact_phone" form:"contact_phone"`
	Type         Type   `json:"type" form:"type"`
}

type ContactResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
