package whatsapp

import (
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// A link created from a view-once "unavailable" placeholder must NOT suppress
// a later delivery of the real content (same wa_message_id, no `unavailable`
// flag) — that delivery upgrades the link. Every other existing link keeps the
// normal dedupe behavior.
func TestChatwootLinkSkipsForward(t *testing.T) {
	placeholderLink := &domainChatStorage.ChatwootMessageLink{
		ChatwootMessageID:     42,
		IsViewOncePlaceholder: true,
	}
	regularLink := &domainChatStorage.ChatwootMessageLink{ChatwootMessageID: 42}
	realPayload := map[string]any{"view_once": true}
	placeholderPayload := map[string]any{"view_once": true, "unavailable": true}

	cases := []struct {
		name     string
		existing *domainChatStorage.ChatwootMessageLink
		data     map[string]any
		want     bool
	}{
		{"no existing link forwards", nil, realPayload, false},
		{"zero-id link forwards", &domainChatStorage.ChatwootMessageLink{}, realPayload, false},
		{"regular duplicate is skipped", regularLink, realPayload, true},
		{"placeholder + real content upgrades (not skipped)", placeholderLink, realPayload, false},
		{"placeholder redelivered as placeholder is skipped", placeholderLink, placeholderPayload, true},
	}
	for _, tc := range cases {
		if got := chatwootLinkSkipsForward(tc.existing, tc.data); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
