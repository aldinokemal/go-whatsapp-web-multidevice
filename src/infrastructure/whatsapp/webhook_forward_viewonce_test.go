package whatsapp

import (
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
)

// isViewOncePlaceholderPayload must require BOTH flags: a delivered view-once
// whose media extraction failed carries view_once alone and is an ordinary
// message, not a privacy withholding.
func TestIsViewOncePlaceholderPayload(t *testing.T) {
	cases := []struct {
		name string
		data map[string]any
		want bool
	}{
		{"both flags", map[string]any{"view_once": true, "unavailable": true}, true},
		{"view_once only", map[string]any{"view_once": true}, false},
		{"unavailable only", map[string]any{"unavailable": true}, false},
		{"neither", map[string]any{}, false},
		{"non-bool values", map[string]any{"view_once": "true", "unavailable": 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isViewOncePlaceholderPayload(tc.data))
		})
	}
}

// The same wa_message_id can be delivered twice: once as the view-once
// "unavailable" placeholder (notice only) and once as the recovered real
// content. decideChatwootLinkAction has to let the content through — including
// when it races the placeholder — without ever producing duplicates.
func TestDecideChatwootLinkAction(t *testing.T) {
	link := func(chatwootID int, placeholder bool) *domainChatStorage.ChatwootMessageLink {
		return &domainChatStorage.ChatwootMessageLink{
			ChatwootMessageID:     chatwootID,
			IsViewOncePlaceholder: placeholder,
		}
	}

	cases := []struct {
		name          string
		existing      *domainChatStorage.ChatwootMessageLink
		isPlaceholder bool
		want          chatwootLinkAction
	}{
		{
			name:     "nothing on file reserves the row first",
			existing: nil,
			want:     chatwootLinkReserve,
		},
		{
			name:          "nothing on file reserves for a placeholder too",
			existing:      nil,
			isPlaceholder: true,
			want:          chatwootLinkReserve,
		},
		{
			name:     "regular duplicate is skipped",
			existing: link(42, false),
			want:     chatwootLinkSkip,
		},
		{
			name:     "linked placeholder + real content claims the upgrade",
			existing: link(42, true),
			want:     chatwootLinkClaimUpgrade,
		},
		{
			name:          "linked placeholder redelivered as placeholder is skipped",
			existing:      link(42, true),
			isPlaceholder: true,
			want:          chatwootLinkSkip,
		},
		{
			name:     "reserved placeholder + real content still forwards",
			existing: link(0, true),
			want:     chatwootLinkForward,
		},
		{
			name:          "reserved real content makes the placeholder redundant",
			existing:      link(0, false),
			isPlaceholder: true,
			want:          chatwootLinkSkip,
		},
		{
			name:     "reserved by an identical delivery: first writer wins",
			existing: link(0, false),
			want:     chatwootLinkSkip,
		},
		{
			name:          "reserved placeholder redelivered as placeholder is skipped",
			existing:      link(0, true),
			isPlaceholder: true,
			want:          chatwootLinkSkip,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, decideChatwootLinkAction(tc.existing, tc.isPlaceholder))
		})
	}
}
