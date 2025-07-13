package send

// ChatPresenceRequest represents a request to send chat presence (typing indicator)
type ChatPresenceRequest struct {
	BaseRequest
	Phone  string `json:"phone" validate:"required"`
	Action string `json:"action" validate:"required"`
}
