package chatstorage

import "time"

// Chat represents a WhatsApp chat/conversation
type Chat struct {
	JID                 string    `db:"jid"`
	Name                string    `db:"name"`
	LastMessageTime     time.Time `db:"last_message_time"`
	EphemeralExpiration uint32    `db:"ephemeral_expiration"`
	CreatedAt           time.Time `db:"created_at"`
	UpdatedAt           time.Time `db:"updated_at"`
}

// Message represents a WhatsApp message
type Message struct {
	ID            string    `db:"id"`
	ChatJID       string    `db:"chat_jid"`
	Sender        string    `db:"sender"`
	Content       string    `db:"content"`
	Timestamp     time.Time `db:"timestamp"`
	IsFromMe      bool      `db:"is_from_me"`
	MediaType     string    `db:"media_type"`
	Filename      string    `db:"filename"`
	URL           string    `db:"url"`
	MediaKey      []byte    `db:"media_key"`
	FileSHA256    []byte    `db:"file_sha256"`
	FileEncSHA256 []byte    `db:"file_enc_sha256"`
	FileLength    uint64    `db:"file_length"`
	CreatedAt     time.Time `db:"created_at"`
	UpdatedAt     time.Time `db:"updated_at"`
}

// MediaInfo represents downloadable media information
type MediaInfo struct {
	MessageID     string
	ChatJID       string
	MediaType     string
	Filename      string
	URL           string
	MediaKey      []byte
	FileSHA256    []byte
	FileEncSHA256 []byte
	FileLength    uint64
}

// MessageFilter represents query filters for messages
type MessageFilter struct {
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
	Limit      int
	Offset     int
	SearchName string
	HasMedia   bool
}
