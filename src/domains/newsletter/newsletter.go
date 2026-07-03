package newsletter

import "context"

type INewsletterUsecase interface {
	Unfollow(ctx context.Context, request UnfollowRequest) (err error)
	GetMessages(ctx context.Context, request GetMessagesRequest) (response GetMessagesResponse, err error)
}

type UnfollowRequest struct {
	NewsletterID string `json:"newsletter_id" form:"newsletter_id"`
}

// GetMessagesRequest fetches the latest messages posted in a newsletter (channel)
// directly from WhatsApp servers.
type GetMessagesRequest struct {
	NewsletterID string `json:"newsletter_id" query:"newsletter_id"`
	// Count limits how many messages are returned. Defaults to 50, capped at 100.
	Count int `json:"count" query:"count"`
	// Before paginates backwards from a given message server ID (exclusive),
	// used to load older messages than the ones already fetched.
	Before int `json:"before" query:"before"`
}

// Message represents a single newsletter (channel) message returned by
// GetMessagesRequest.
type Message struct {
	ServerID       int            `json:"server_id"`
	MessageID      string         `json:"message_id"`
	Type           string         `json:"type"`
	Timestamp      string         `json:"timestamp"`
	ViewsCount     int            `json:"views_count"`
	ReactionCounts map[string]int `json:"reaction_counts,omitempty"`
	Text           string         `json:"text,omitempty"`
}

type GetMessagesResponse struct {
	Data []Message `json:"data"`
}
