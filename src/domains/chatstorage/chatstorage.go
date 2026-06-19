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
type DeviceRecord struct {
	DeviceID    string    `db:"device_id"`
	DisplayName string    `db:"display_name"`
	JID         string    `db:"jid"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
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
