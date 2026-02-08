package chatwoot

type Contact struct {
	ID               int                    `json:"id"`
	Name             string                 `json:"name"`
	Email            string                 `json:"email"`
	PhoneNumber      string                 `json:"phone_number"`
	Identifier       string                 `json:"identifier"`
	CustomAttributes map[string]interface{} `json:"custom_attributes"`
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
	InboxID          int                    `json:"inbox_id"`
	Name             string                 `json:"name"`
	PhoneNumber      string                 `json:"phone_number,omitempty"`
	Identifier       string                 `json:"identifier,omitempty"`
	CustomAttributes map[string]interface{} `json:"custom_attributes"`
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
}

type WebhookPayload struct {
	ID           int                 `json:"id"`
	Event        string              `json:"event"`
	MessageType  string              `json:"message_type"`
	Content      string              `json:"content"`
	Private      bool                `json:"private"`
	Account      Account             `json:"account"`
	Conversation ConversationWebhook `json:"conversation"`
	Sender       Contact             `json:"sender"`
	Attachments  []Attachment        `json:"attachments"`
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
