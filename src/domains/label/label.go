package label

import "context"

// ILabelUsecase manages WhatsApp labels: the labels of Business accounts and
// the chat lists of personal accounts.
type ILabelUsecase interface {
	// ListLabels returns the labels this device knows from app state, with
	// their chats. Deleted labels are left out.
	ListLabels(ctx context.Context) (ListLabelsResponse, error)
	// ResyncLabels fetches every label and label chat from WhatsApp again,
	// replaces the stored copy and returns it.
	ResyncLabels(ctx context.Context) (ListLabelsResponse, error)
	// EditLabel creates, renames or deletes a label.
	EditLabel(ctx context.Context, request EditLabelRequest) (EditLabelResponse, error)
	// LabelChat adds a chat to a label or removes it.
	LabelChat(ctx context.Context, request LabelChatRequest) (LabelChatResponse, error)
}

// LabelTypeCustom is the label type of the chat lists of personal accounts.
const LabelTypeCustom = "CUSTOM"

type EditLabelRequest struct {
	LabelID string `json:"label_id"`
	Name    string `json:"name"`
	// Color is an index in WhatsApp's label palette. When omitted, an edit
	// keeps the stored color (0 for a new label).
	Color *int32 `json:"color"`
	// Type is empty for a Business label or "CUSTOM" for a chat list of a
	// personal account. When omitted, an edit keeps the stored type.
	Type string `json:"type"`
	// OrderIndex is the position of the label. When omitted, an edit keeps the
	// stored position.
	OrderIndex *int32 `json:"order_index"`
	Deleted    bool   `json:"deleted"`
}

type EditLabelResponse struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	LabelID    string `json:"label_id"`
	Name       string `json:"name"`
	Color      int32  `json:"color"`
	Type       string `json:"type,omitempty"`
	OrderIndex int32  `json:"order_index"`
	Deleted    bool   `json:"deleted"`
}

type LabelChatRequest struct {
	ChatJID string `json:"chat_jid" uri:"chat_jid"`
	LabelID string `json:"label_id"`
	// Labeled is required: true adds the chat to the label, false removes it.
	Labeled *bool `json:"labeled"`
}

type LabelChatResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	ChatJID string `json:"chat_jid"`
	LabelID string `json:"label_id"`
	Labeled bool   `json:"labeled"`
}

type LabelInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Color      int32    `json:"color"`
	Type       string   `json:"type,omitempty"`
	OrderIndex int32    `json:"order_index"`
	Active     bool     `json:"active"`
	Chats      []string `json:"chats"`
	UpdatedAt  string   `json:"updated_at"`
}

type ListLabelsResponse struct {
	Data []LabelInfo `json:"data"`
}
