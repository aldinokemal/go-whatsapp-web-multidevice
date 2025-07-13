package send

type GenericResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}
