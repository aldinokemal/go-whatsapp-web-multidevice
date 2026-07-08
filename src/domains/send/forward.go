package send

type ForwardRequest struct {
	MessageID     string `json:"message_id" form:"message_id"`
	Phone         string `json:"phone" form:"phone"`
	Duration      *int   `json:"duration,omitempty" form:"duration"`
	ForceReupload bool   `json:"force_reupload,omitempty" form:"force_reupload"`
}
