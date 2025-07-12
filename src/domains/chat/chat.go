package chat

// Request and Response structures for chat operations

type ListChatsRequest struct {
	Limit    int    `json:"limit" query:"limit"`
	Offset   int    `json:"offset" query:"offset"`
	Search   string `json:"search" query:"search"`
	HasMedia bool   `json:"has_media" query:"has_media"`
}

type ListChatsResponse struct {
	Data       []ChatInfo         `json:"data"`
	Pagination PaginationResponse `json:"pagination"`
}

type GetChatMessagesRequest struct {
	ChatJID   string  `json:"chat_jid" uri:"chat_jid"`
	Limit     int     `json:"limit" query:"limit"`
	Offset    int     `json:"offset" query:"offset"`
	StartTime *string `json:"start_time" query:"start_time"`
	EndTime   *string `json:"end_time" query:"end_time"`
	MediaOnly bool    `json:"media_only" query:"media_only"`
	IsFromMe  *bool   `json:"is_from_me" query:"is_from_me"`
	Search    string  `json:"search" query:"search"`
}

type GetChatMessagesResponse struct {
	Data       []MessageInfo      `json:"data"`
	Pagination PaginationResponse `json:"pagination"`
	ChatInfo   ChatInfo           `json:"chat_info"`
}

// Pin Chat operations
type PinChatRequest struct {
	ChatJID string `json:"chat_jid" uri:"chat_jid"`
	Pinned  bool   `json:"pinned"`
}

type PinChatResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	ChatJID string `json:"chat_jid"`
	Pinned  bool   `json:"pinned"`
}

type ChatInfo struct {
	JID                 string `json:"jid"`
	Name                string `json:"name"`
	LastMessageTime     string `json:"last_message_time"`
	EphemeralExpiration uint32 `json:"ephemeral_expiration"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

type MessageInfo struct {
	ID         string `json:"id"`
	ChatJID    string `json:"chat_jid"`
	SenderJID  string `json:"sender_jid"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp"`
	IsFromMe   bool   `json:"is_from_me"`
	MediaType  string `json:"media_type"`
	Filename   string `json:"filename"`
	URL        string `json:"url"`
	FileLength uint64 `json:"file_length"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type PaginationResponse struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}
