package newsletter

import (
	"context"

	"go.mau.fi/whatsmeow/types"
)

type INewsletterUsecase interface {
	Follow(ctx context.Context, request FollowRequest) (err error)
	Unfollow(ctx context.Context, request UnfollowRequest) (err error)
	GetInfo(ctx context.Context, request GetInfoRequest) (response types.NewsletterMetadata, err error)
	GetMessages(ctx context.Context, request GetMessagesRequest) (response GetMessagesResponse, err error)
	GetMessageUpdates(ctx context.Context, request GetMessageUpdatesRequest) (response GetMessagesResponse, err error)
	SubscribeLiveUpdates(ctx context.Context, request LiveUpdatesRequest) (response LiveUpdatesSubscription, err error)
	UnsubscribeLiveUpdates(ctx context.Context, request LiveUpdatesRequest) (err error)
	ListLiveUpdates(ctx context.Context) (response ListLiveUpdatesResponse, err error)
	Directory(ctx context.Context, request DirectoryRequest) (response DirectoryResponse, err error)
	DownloadMedia(ctx context.Context, request DownloadMediaRequest) (response DownloadMediaResponse, err error)
}

type FollowRequest struct {
	NewsletterID string `json:"newsletter_id" form:"newsletter_id"`
}

type UnfollowRequest struct {
	NewsletterID string `json:"newsletter_id" form:"newsletter_id"`
}

// GetInfoRequest looks up a newsletter (channel) either by JID or by invite
// link/code. Exactly one of the two must be set. The invite lookup works
// without following the channel.
type GetInfoRequest struct {
	NewsletterID string `json:"newsletter_id" query:"newsletter_id"`
	// Invite accepts the full link (https://whatsapp.com/channel/...) or just the code.
	Invite string `json:"invite" query:"invite"`
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

// GetMessageUpdatesRequest fetches only the counters (views, forwards and
// reactions) of newsletter messages, without the message content.
type GetMessageUpdatesRequest struct {
	NewsletterID string `json:"newsletter_id" query:"newsletter_id"`
	// Count limits how many message updates are returned. Defaults to 50, capped at 100.
	Count int `json:"count" query:"count"`
	// Since only returns messages whose counters changed after this Unix timestamp (seconds).
	Since int64 `json:"since" query:"since"`
	// After is required: updates are returned for messages with a server ID
	// greater than this one.
	After int `json:"after" query:"after"`
}

// Message represents a single newsletter (channel) message returned by
// GetMessagesRequest or GetMessageUpdatesRequest.
type Message struct {
	ServerID       int            `json:"server_id"`
	MessageID      string         `json:"message_id"`
	Type           string         `json:"type"`
	Timestamp      string         `json:"timestamp"`
	ViewsCount     int            `json:"views_count"`
	ForwardsCount  int            `json:"forwards_count"`
	ReactionCounts map[string]int `json:"reaction_counts,omitempty"`
	Text           string         `json:"text,omitempty"`
}

type GetMessagesResponse struct {
	Data []Message `json:"data"`
}

type LiveUpdatesRequest struct {
	NewsletterID string `json:"newsletter_id" form:"newsletter_id" query:"newsletter_id"`
}

// LiveUpdatesSubscription describes an active live updates subscription. The
// server grants it for a limited duration; GOWA renews it before it expires
// until it is stopped or the process exits.
type LiveUpdatesSubscription struct {
	NewsletterID    string `json:"newsletter_id"`
	DurationSeconds int    `json:"duration_seconds"`
	ExpiresAt       string `json:"expires_at"`
	RenewedAt       string `json:"renewed_at"`
	LastError       string `json:"last_error,omitempty"`
}

type ListLiveUpdatesResponse struct {
	Data []LiveUpdatesSubscription `json:"data"`
}

// DirectoryRequest searches the public channel directory (the "Find channels"
// screen of the official clients).
type DirectoryRequest struct {
	// View selects the directory list: RECOMMENDED, TRENDING, POPULAR, NEW or FEATURED.
	// Ignored when SearchText is set.
	View string `json:"view" query:"view"`
	// SearchText searches channels by name.
	SearchText string `json:"search_text" query:"search_text"`
	// CountryCodes filters by ISO 3166-1 alpha-2 country codes (e.g. BR, ID).
	// Required to browse a view; the search query does not support it.
	CountryCodes []string `json:"country_codes" query:"country_codes"`
	// Categories filters by BUSINESS, ENTERTAINMENT, LIFESTYLE, NEWS,
	// ORGANIZATIONS, PEOPLE or SPORTS.
	Categories []string `json:"categories" query:"categories"`
	// Limit caps how many channels are returned. Defaults to 20, capped at 50.
	Limit int `json:"limit" query:"limit"`
	// Cursor continues a previous page (the next_cursor from the last response).
	Cursor string `json:"cursor" query:"cursor"`
}

type DirectoryResponse struct {
	Data       []types.NewsletterMetadata `json:"data"`
	NextCursor string                     `json:"next_cursor,omitempty"`
	HasMore    bool                       `json:"has_more"`
}

type DownloadMediaRequest struct {
	NewsletterID string `json:"newsletter_id" query:"newsletter_id"`
	ServerID     int    `json:"server_id" uri:"server_id"`
}

type DownloadMediaResponse struct {
	ServerID  int    `json:"server_id"`
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
	MediaType string `json:"media_type"`
	MimeType  string `json:"mime_type"`
	Filename  string `json:"filename"`
	FilePath  string `json:"file_path"`
	FileURL   string `json:"file_url,omitempty"`
	FileSize  int64  `json:"file_size"`
}
