package chat

// ReactionInfo represents a single emoji reaction attached to a message.
type ReactionInfo struct {
	Emoji      string `json:"emoji"`
	SenderJID  string `json:"sender_jid"`
	IsFromMe   bool   `json:"is_from_me"`
	Timestamp  string `json:"timestamp"`
}
