package chatwoot

type Contact struct {
	ID               int            `json:"id"`
	Name             string         `json:"name"`
	Email            string         `json:"email"`
	PhoneNumber      string         `json:"phone_number"`
	Identifier       string         `json:"identifier"`
	CustomAttributes map[string]any `json:"custom_attributes"`
}

type Conversation struct {
	ID        int    `json:"id"`
	ContactID int    `json:"contact_id"`
	InboxID   int    `json:"inbox_id"`
	Status    string `json:"status"`
}

type Message struct {
	ID          int    `json:"id"`
	Content     string `json:"content"`
	MessageType string `json:"message_type"`
	Private     bool   `json:"private"`
	ContentType string `json:"content_type"`
}

type CreateContactRequest struct {
	InboxID          int            `json:"inbox_id"`
	Name             string         `json:"name"`
	PhoneNumber      string         `json:"phone_number,omitempty"`
	Identifier       string         `json:"identifier,omitempty"`
	CustomAttributes map[string]any `json:"custom_attributes"`
}

type CreateConversationRequest struct {
	InboxID   int    `json:"inbox_id"`
	ContactID int    `json:"contact_id"`
	Status    string `json:"status"`
	// SourceID is the WhatsApp chat JID. Sending it keys the conversation's
	// contact_inbox by that JID so the contact public endpoints can resolve the
	// conversation; empty is omitted so Chatwoot generates one.
	SourceID string `json:"source_id,omitempty"`
}

type CreateMessageRequest struct {
	Content           string         `json:"content"`
	MessageType       string         `json:"message_type"`
	Private           bool           `json:"private"`
	SourceID          string         `json:"source_id,omitempty"`
	ContentAttributes map[string]any `json:"content_attributes,omitempty"`
}

// Inbox is the subset of a Chatwoot inbox we read during auto-provisioning.
type Inbox struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	ChannelType     string `json:"channel_type"`
	InboxIdentifier string `json:"inbox_identifier"`
}

// CreateInboxRequest provisions an API-channel inbox. Chatwoot nests the
// channel descriptor under "channel"; type "api" with a webhook_url makes
// Chatwoot POST agent replies to that URL.
type CreateInboxRequest struct {
	Name    string             `json:"name"`
	Channel CreateInboxChannel `json:"channel"`
}

type CreateInboxChannel struct {
	Type       string `json:"type"`
	WebhookURL string `json:"webhook_url,omitempty"`
}

type WebhookPayload struct {
	ID                int                 `json:"id"`
	Event             string              `json:"event"`
	MessageType       string              `json:"message_type"`
	Content           string              `json:"content"`
	Private           bool                `json:"private"`
	SourceID          string              `json:"source_id"`
	ContentAttributes map[string]any      `json:"content_attributes"`
	Account           Account             `json:"account"`
	Conversation      ConversationWebhook `json:"conversation"`
	Sender            Contact             `json:"sender"`
	Attachments       []Attachment        `json:"attachments"`
}

type Attachment struct {
	ID        int    `json:"id"`
	FileType  string `json:"file_type"`
	DataURL   string `json:"data_url"`
	ThumbURL  string `json:"thumb_url"`
	Extension string `json:"extension"`
}

type ConversationWebhook struct {
	ID   int              `json:"id"`
	Meta ConversationMeta `json:"meta"`
}

type ConversationMeta struct {
	Sender Contact `json:"sender"`
}

type Account struct {
	ID int `json:"id"`
}
