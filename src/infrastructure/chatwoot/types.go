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

type Inbox struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
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
}

type CreateMessageRequest struct {
	Content     string `json:"content"`
	MessageType string `json:"message_type"`
	Private     bool   `json:"private"`
	// SourceID stamps the originating WhatsApp message ID so Chatwoot can later
	// resolve reply threading (in_reply_to_external_id) back to this message.
	SourceID string `json:"source_id,omitempty"`
	// ContentAttributes carries extra fields like in_reply_to_external_id to
	// thread a reply to the quoted message.
	ContentAttributes map[string]any `json:"content_attributes,omitempty"`
}

type WebhookPayload struct {
	ID                int                      `json:"id"`
	Event             string                   `json:"event"`
	MessageType       string                   `json:"message_type"`
	Content           string                   `json:"content"`
	Private           bool                     `json:"private"`
	IsPrivate         bool                     `json:"is_private"` // typing events use is_private
	Account           Account                  `json:"account"`
	Conversation      ConversationWebhook      `json:"conversation"`
	Sender            Contact                  `json:"sender"`
	Attachments       []Attachment             `json:"attachments"`
	ContentAttributes WebhookContentAttributes `json:"content_attributes"`
}

// WebhookContentAttributes holds reply-threading info Chatwoot sends on an agent
// reply. InReplyToExternalID is the source_id (WhatsApp message ID) of the quoted
// message; InReplyTo is Chatwoot's internal message ID of the quoted message.
type WebhookContentAttributes struct {
	InReplyTo           int    `json:"in_reply_to"`
	InReplyToExternalID string `json:"in_reply_to_external_id"`
}

type Attachment struct {
	ID        int    `json:"id"`
	FileType  string `json:"file_type"`
	DataURL   string `json:"data_url"`
	ThumbURL  string `json:"thumb_url"`
	Extension string `json:"extension"`
}

type ConversationWebhook struct {
	ID      int              `json:"id"`
	InboxID int              `json:"inbox_id"`
	Meta    ConversationMeta `json:"meta"`
}

type ConversationMeta struct {
	Sender Contact `json:"sender"`
}

type Account struct {
	ID int `json:"id"`
}
