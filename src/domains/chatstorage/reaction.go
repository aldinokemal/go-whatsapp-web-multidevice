package chatstorage

import "time"

// Reaction represents a single reaction stored alongside a message.
type Reaction struct {
	MessageID    string    `db:"message_id"`
	ChatJID      string    `db:"chat_jid"`
	DeviceID     string    `db:"device_id"`
	ReactorJID   string    `db:"reactor_jid"`
	Emoji        string    `db:"emoji"`
	IsFromMe     bool      `db:"is_from_me"`
	Timestamp    time.Time `db:"reaction_timestamp"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}
