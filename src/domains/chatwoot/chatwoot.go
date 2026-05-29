package chatwoot

// DeviceConfig maps a single WhatsApp device (JID) to a Chatwoot inbox.
// It is the per-device equivalent of the global CHATWOOT_* environment variables,
// enabling multi-device / multi-inbox routing.
type DeviceConfig struct {
	ID             int    `json:"id"`
	DeviceID       string `json:"device_id"`
	ChatwootURL    string `json:"chatwoot_url"`
	APIToken       string `json:"api_token"`
	AccountID      int    `json:"account_id"`
	InboxID        int    `json:"inbox_id"`
	Enabled        bool   `json:"enabled"`
	ImportMessages bool   `json:"import_messages"`
	DaysLimit      int    `json:"days_limit"`
}
