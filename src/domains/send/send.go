package send

type GenericResponse struct {
	MessageID   string `json:"message_id,omitempty"`
	Status      string `json:"status"`
	ScheduleID  string `json:"schedule_id,omitempty"`
	ScheduledAt string `json:"scheduled_at,omitempty"`
	NextRunAt   string `json:"next_run_at,omitempty"`

	// RecipientJID is the validated destination used for a delivered text or attachment.
	RecipientJID string `json:"-"`
}
