package chatstorage

import "time"

// Chat represents a WhatsApp chat/conversation
type Chat struct {
	DeviceID            string    `db:"device_id"`
	JID                 string    `db:"jid"`
	Name                string    `db:"name"`
	LastMessageTime     time.Time `db:"last_message_time"`
	EphemeralExpiration uint32    `db:"ephemeral_expiration"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
	Archived            bool      `db:"archived"`
}

// Message represents a WhatsApp message
type Message struct {
	ID               string     `db:"id"`
	ChatJID          string     `db:"chat_jid"`
	DeviceID         string     `db:"device_id"`
	Sender           string     `db:"sender"`
	Content          string     `db:"content"`
	Timestamp        time.Time  `db:"timestamp"`
	IsFromMe         bool       `db:"is_from_me"`
	MediaType        string     `db:"media_type"`
	CallMetadata     string     `db:"call_metadata"`
	Filename         string     `db:"filename"`
	URL              string     `db:"url"`
	DirectPath       string     `db:"direct_path"`
	MediaKey         []byte     `db:"media_key"`
	FileSHA256       []byte     `db:"file_sha256"`
	FileEncSHA256    []byte     `db:"file_enc_sha256"`
	FileLength       uint64     `db:"file_length"`
	ReferralMetadata string     `db:"referral_metadata"`
	Reactions        []Reaction `db:"-"`
	CreatedAt        time.Time  `db:"created_at"`
	UpdatedAt        time.Time  `db:"updated_at"`
}

// MessageEdit represents a single edit applied to an existing WhatsApp message.
type MessageEdit struct {
	OriginalMessageID string    `db:"original_message_id"`
	EditEventID       string    `db:"edit_event_id"`
	ChatJID           string    `db:"chat_jid"`
	DeviceID          string    `db:"device_id"`
	Editor            string    `db:"editor"`
	PreviousContent   string    `db:"previous_content"`
	NewContent        string    `db:"new_content"`
	EditedAt          time.Time `db:"edited_at"`
	CreatedAt         time.Time `db:"created_at"`
}

// ChatwootMessageLink maps a WhatsApp message to the Chatwoot message created
// for it. It is device-scoped because the same WhatsApp message ID can appear
// in independent device stores.
type ChatwootMessageLink struct {
	DeviceID                     string    `db:"device_id"`
	WhatsAppMessageID            string    `db:"wa_message_id"`
	WhatsAppChatJID              string    `db:"wa_chat_jid"`
	ChatwootMessageID            int       `db:"chatwoot_message_id"`
	ChatwootConversationID       int       `db:"chatwoot_conversation_id"`
	ChatwootInboxID              int       `db:"chatwoot_inbox_id"`
	ChatwootContactInboxSourceID string    `db:"chatwoot_contact_inbox_source_id"`
	SourceID                     string    `db:"source_id"`
	Direction                    string    `db:"direction"`
	IsRead                       bool      `db:"is_read"`
	CreatedAt                    time.Time `db:"created_at"`
	UpdatedAt                    time.Time `db:"updated_at"`
	// ChatwootConfigID is the id of the chatwoot_device_configs row this link
	// belongs to. 0 means the legacy/env config (single-account). It scopes
	// reverse routing so conversation/message ids cannot collide across accounts.
	ChatwootConfigID int64 `db:"chatwoot_config_id"`
	// ChatwootAccountID is the resolved Chatwoot account id, denormalized so the
	// conversation lookup can be account-scoped without a join. 0 = legacy.
	ChatwootAccountID int `db:"chatwoot_account_id"`
}

// ChatwootDeviceConfig is the per-device Chatwoot destination (URL + account +
// inbox + token). It enables routing each WhatsApp device to its own Chatwoot
// inbox. DeviceID is the user-facing device id; DeviceJID mirrors the WhatsApp
// storage JID so the registry can resolve a client from either identity (the
// forward/link paths key on the JID, the REST/reverse paths on the device id).
type ChatwootDeviceConfig struct {
	ID          int64     `db:"id"`
	DeviceID    string    `db:"device_id"`
	DeviceJID   string    `db:"device_jid"`
	ChatwootURL string    `db:"chatwoot_url"`
	AccountID   int       `db:"account_id"`
	InboxID     int       `db:"inbox_id"`
	APIToken    string    `db:"api_token"`
	Enabled     bool      `db:"enabled"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// ChatwootForwardEvent is a durable retry record for a live WhatsApp event
// that could not be delivered to Chatwoot because of a transient failure.
type ChatwootForwardEvent struct {
	ID                int64     `db:"id"`
	DeviceID          string    `db:"device_id"`
	EventName         string    `db:"event_name"`
	WhatsAppMessageID string    `db:"wa_message_id"`
	PayloadJSON       string    `db:"payload_json"`
	Attempts          int       `db:"attempts"`
	LastError         string    `db:"last_error"`
	NextAttemptAt     time.Time `db:"next_attempt_at"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
}

// MediaInfo represents downloadable media information
type MediaInfo struct {
	MessageID     string
	ChatJID       string
	MediaType     string
	Filename      string
	URL           string
	DirectPath    string
	MediaKey      []byte
	FileSHA256    []byte
	FileEncSHA256 []byte
	FileLength    uint64
}

// DeviceRecord tracks a registered device for persistence purposes.
// JID holds the bare-number (NonAD) form used for chat storage partitioning; ADJID
// holds the full companion identity (number:NN@s.whatsapp.net) that pins the slot to
// one specific whatsmeow session. ADJID is empty until the first connect after
// pairing (the :NN suffix is only known once the socket authenticates).
type DeviceRecord struct {
	DeviceID                  string    `db:"device_id"`
	DisplayName               string    `db:"display_name"`
	JID                       string    `db:"jid"`
	ADJID                     string    `db:"ad_jid"`
	WebhookURL                *string   `db:"webhook_url"`
	WebhookSecret             string    `db:"webhook_secret"`
	WebhookEvents             string    `db:"webhook_events"`
	WebhookInsecureSkipVerify bool      `db:"webhook_insecure_skip_verify"`
	CreatedAt                 time.Time `db:"created_at"`
	UpdatedAt                 time.Time `db:"updated_at"`
}

// DeviceWebhookConfig holds the complete webhook configuration for a device.
type DeviceWebhookConfig struct {
	WebhookURL                *string `json:"webhook_url,omitempty"`
	WebhookSecret             string  `json:"webhook_secret,omitempty"`
	WebhookEvents             string  `json:"webhook_events,omitempty"`
	WebhookInsecureSkipVerify bool    `json:"webhook_insecure_skip_verify,omitempty"`
}

// MessageFilter represents query filters for messages
type MessageFilter struct {
	DeviceID  string
	ChatJID   string
	Limit     int
	Offset    int
	StartTime *time.Time
	EndTime   *time.Time
	MediaOnly bool
	IsFromMe  *bool
}

// ChatFilter represents query filters for chats
type ChatFilter struct {
	DeviceID   string
	Limit      int
	Offset     int
	SearchName string
	HasMedia   bool
	IsArchived *bool
}
