package message

type GenericResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

type RevokeRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type DeleteRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type ReactionRequest struct {
	MessageID string `json:"message_id" form:"message_id"`
	Phone     string `json:"phone" form:"phone"`
	Emoji     string `json:"emoji" form:"emoji"`
}

type UpdateMessageRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Message   string `json:"message" form:"message"`
	Phone     string `json:"phone" form:"phone"`
	// Link, when set, sends the edit with a rich link preview built from this URL.
	Link string `json:"link,omitempty" form:"link"`
	// MediaType, when "image" or "document", sends the edit as the new caption of
	// that media message. Leave it empty to edit a text message.
	MediaType string `json:"media_type,omitempty" form:"media_type"`
}

type MarkAsReadRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

// MarkAsPlayedRequest has the same transport fields as a read receipt, but
// requests WhatsApp's played receipt for an existing voice message.
type MarkAsPlayedRequest = MarkAsReadRequest

type StarRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
	IsStarred bool   `json:"is_starred"`
}

type DownloadMediaRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type DownloadMediaResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename"`
	FilePath  string `json:"file_path"`
	FileURL   string `json:"file_url,omitempty"`
	FileSize  int64  `json:"file_size"`
}
