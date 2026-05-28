package webhook

// DeviceWebhookConfig binds a single WhatsApp device (JID) to one webhook target.
// It is the per-device equivalent of the global WHATSAPP_WEBHOOK environment
// variable: a device may own several configs (1:N), each with its own URL, HMAC
// secret, event filter and custom headers. An empty Events list means "all events".
type DeviceWebhookConfig struct {
	ID         int               `json:"id"`
	DeviceID   string            `json:"device_id"`
	WebhookURL string            `json:"webhook_url"`
	Secret     string            `json:"secret"`
	Events     []string          `json:"events"`
	Enabled    bool              `json:"enabled"`
	Headers    map[string]string `json:"headers"`
}
