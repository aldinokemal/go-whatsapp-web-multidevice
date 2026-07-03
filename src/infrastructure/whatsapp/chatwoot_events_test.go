package whatsapp

import (
	"context"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// TestBuildEditDeleteChatwootContent pins how WhatsApp edit/revoke/delete events
// render into the Chatwoot note text and the WhatsApp message id used for
// threading. The exact emoji-prefixed strings are agent-facing contract, and the
// threadID is what anchors the note onto the message it refers to — a wrong key
// here detaches the note in the inbox. The edited body is run through the WA->CW
// markdown translator, so emphasis added in the original edit survives the hop.
func TestBuildEditDeleteChatwootContent(t *testing.T) {
	t.Run("edited with plain body", func(t *testing.T) {
		// Happy path: a non-empty edit body is wrapped in the "Edited:" prefix and
		// the original message id is returned for threading.
		content, threadID := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body":                "new text",
			"original_message_id": "wamid-edit-1",
		}, false, "")
		if content != "✏️ **Edited:** new text" {
			t.Fatalf("content = %q", content)
		}
		if threadID != "wamid-edit-1" {
			t.Fatalf("threadID = %q", threadID)
		}
	})

	t.Run("edited body is markdown-translated", func(t *testing.T) {
		// WhatsApp markup in the edited body must be translated to Chatwoot/GFM so
		// "*hi*" renders as bold rather than literal asterisks. This is the whole
		// reason WhatsAppToChatwootMarkdown is called in the edit branch.
		content, _ := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body":                "*hi*",
			"original_message_id": "wamid-edit-md",
		}, false, "")
		if content != "✏️ **Edited:** **hi**" {
			t.Fatalf("content = %q", content)
		}
		// Defensive: the translated markdown must appear inside the rendered note.
		if !strings.Contains(content, "**hi**") {
			t.Fatalf("expected translated bold in content, got %q", content)
		}
	})

	t.Run("edited body mixed markup translated", func(t *testing.T) {
		// Bold + italic together exercise the sentinel ordering in the translator:
		// "*bold* _it_" -> "**bold** *it*".
		content, _ := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body":                "*bold* _it_",
			"original_message_id": "wamid-edit-mix",
		}, false, "")
		if content != "✏️ **Edited:** **bold** *it*" {
			t.Fatalf("content = %q", content)
		}
	})

	t.Run("edited group prepends sender after translation", func(t *testing.T) {
		// In a group the sender name is prefixed onto the (already translated) body
		// before the "Edited:" wrapper, so the prefix sits inside the note body.
		content, threadID := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body":                "*hi*",
			"original_message_id": "wamid-edit-grp",
		}, true, "Alice")
		if content != "✏️ **Edited:** Alice: **hi**" {
			t.Fatalf("content = %q", content)
		}
		if threadID != "wamid-edit-grp" {
			t.Fatalf("threadID = %q", threadID)
		}
	})

	t.Run("edited group with empty fromName does not prefix", func(t *testing.T) {
		// Guard: empty sender name in a group must not produce a stray ": " prefix.
		content, _ := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body":                "hello",
			"original_message_id": "wamid-edit-noname",
		}, true, "")
		if content != "✏️ **Edited:** hello" {
			t.Fatalf("content = %q", content)
		}
	})

	t.Run("edited non-group ignores fromName", func(t *testing.T) {
		// The sender prefix is group-only; a 1:1 edit must never be prefixed even
		// when a fromName is present.
		content, _ := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body":                "solo",
			"original_message_id": "wamid-edit-1to1",
		}, false, "Bob")
		if content != "✏️ **Edited:** solo" {
			t.Fatalf("content = %q", content)
		}
	})

	t.Run("edited with empty body yields placeholder", func(t *testing.T) {
		// An edit event with no body (e.g. the new content couldn't be extracted)
		// still produces a renderable "message edited" note rather than dropping.
		content, threadID := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body":                "",
			"original_message_id": "wamid-edit-empty",
		}, false, "")
		if content != "✏️ _(message edited)_" {
			t.Fatalf("content = %q", content)
		}
		// threadID is still threaded onto the original message even with no body.
		if threadID != "wamid-edit-empty" {
			t.Fatalf("threadID = %q", threadID)
		}
	})

	t.Run("edited with whitespace-only body yields placeholder", func(t *testing.T) {
		// The body is TrimSpace'd before the empty check, so a whitespace-only edit
		// collapses to the same placeholder as an empty one.
		content, _ := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body":                "   \t  ",
			"original_message_id": "wamid-edit-ws",
		}, false, "")
		if content != "✏️ _(message edited)_" {
			t.Fatalf("content = %q", content)
		}
	})

	t.Run("edited group with whitespace-only body uses placeholder not prefix", func(t *testing.T) {
		// After trim the body is empty, so the group prefix branch (guarded on
		// body != "") is skipped and the placeholder is returned unprefixed.
		content, _ := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body":                "   ",
			"original_message_id": "wamid-edit-ws-grp",
		}, true, "Alice")
		if content != "✏️ _(message edited)_" {
			t.Fatalf("content = %q", content)
		}
	})

	t.Run("edited with missing body key yields placeholder", func(t *testing.T) {
		// No "body" key at all takes the same path as empty string (assertion -> "").
		content, threadID := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"original_message_id": "wamid-edit-nobody",
		}, false, "")
		if content != "✏️ _(message edited)_" {
			t.Fatalf("content = %q", content)
		}
		if threadID != "wamid-edit-nobody" {
			t.Fatalf("threadID = %q", threadID)
		}
	})

	t.Run("edited with missing original_message_id yields empty threadID", func(t *testing.T) {
		// Without the original id the note still renders, but threading is impossible
		// so threadID is empty and the caller skips the in_reply_to wiring.
		content, threadID := buildEditDeleteChatwootContent("message.edited", map[string]any{
			"body": "text",
		}, false, "")
		if content != "✏️ **Edited:** text" {
			t.Fatalf("content = %q", content)
		}
		if threadID != "" {
			t.Fatalf("expected empty threadID, got %q", threadID)
		}
	})

	t.Run("revoked yields delete note and revoked id", func(t *testing.T) {
		// A revoke (sender deleted for everyone) renders the fixed delete note and
		// threads on revoked_message_id.
		content, threadID := buildEditDeleteChatwootContent("message.revoked", map[string]any{
			"revoked_message_id": "wamid-rev-1",
		}, false, "")
		if content != "🗑️ _This message was deleted._" {
			t.Fatalf("content = %q", content)
		}
		if threadID != "wamid-rev-1" {
			t.Fatalf("threadID = %q", threadID)
		}
	})

	t.Run("revoked ignores group/fromName", func(t *testing.T) {
		// The delete note is fixed text; group flag and sender name never alter it.
		content, _ := buildEditDeleteChatwootContent("message.revoked", map[string]any{
			"revoked_message_id": "wamid-rev-grp",
		}, true, "Alice")
		if content != "🗑️ _This message was deleted._" {
			t.Fatalf("content = %q", content)
		}
	})

	t.Run("revoked with missing id yields empty threadID", func(t *testing.T) {
		content, threadID := buildEditDeleteChatwootContent("message.revoked", map[string]any{}, false, "")
		if content != "🗑️ _This message was deleted._" {
			t.Fatalf("content = %q", content)
		}
		if threadID != "" {
			t.Fatalf("expected empty threadID, got %q", threadID)
		}
	})

	t.Run("deleted yields same delete note and deleted id", func(t *testing.T) {
		// message.deleted (deleted-for-me) shares the revoke note text but reads its
		// id from deleted_message_id, a distinct key.
		content, threadID := buildEditDeleteChatwootContent("message.deleted", map[string]any{
			"deleted_message_id": "wamid-del-1",
		}, false, "")
		if content != "🗑️ _This message was deleted._" {
			t.Fatalf("content = %q", content)
		}
		if threadID != "wamid-del-1" {
			t.Fatalf("threadID = %q", threadID)
		}
	})

	t.Run("deleted reads deleted_message_id not revoked_message_id", func(t *testing.T) {
		// Pin the key separation: a deleted event must NOT pick up revoked_message_id
		// even when it is present in the payload.
		_, threadID := buildEditDeleteChatwootContent("message.deleted", map[string]any{
			"deleted_message_id": "wamid-del-2",
			"revoked_message_id": "wamid-should-be-ignored",
		}, false, "")
		if threadID != "wamid-del-2" {
			t.Fatalf("expected deleted_message_id to win, got %q", threadID)
		}
	})

	t.Run("unknown event yields empty content and threadID", func(t *testing.T) {
		// Any event the function doesn't handle returns ("","") so the caller skips
		// it rather than emitting a blank note.
		content, threadID := buildEditDeleteChatwootContent("message.reaction", map[string]any{
			"body": "ignored",
		}, false, "")
		if content != "" || threadID != "" {
			t.Fatalf("expected empty pair for unknown event, got (%q, %q)", content, threadID)
		}
	})

	t.Run("plain message event is not handled here", func(t *testing.T) {
		// The base "message" event is rendered by buildChatwootMessageContent, not
		// this function, so it falls through to the empty default.
		content, threadID := buildEditDeleteChatwootContent("message", map[string]any{
			"body": "hi",
		}, false, "")
		if content != "" || threadID != "" {
			t.Fatalf("expected empty pair for 'message', got (%q, %q)", content, threadID)
		}
	})
}

// TestExtractChatwootContactInfoIgnoreJids pins the CHATWOOT_IGNORE_JIDS branch of
// extractChatwootContactInfo. This is operator-facing access control: a JID in the
// ignore list must be rejected (error, nil info) before any contact is created, so
// muted groups/numbers never spawn a Chatwoot conversation. The global slice is
// saved and restored on every case so sibling tests see the default (empty) list.
// All cases use context.Background(), where getGroupName has no WA client.
func TestExtractChatwootContactInfoIgnoreJids(t *testing.T) {
	ctx := context.Background()
	original := config.ChatwootIgnoreJids
	defer func() { config.ChatwootIgnoreJids = original }()

	t.Run("@g.us wildcard rejects a group chat", func(t *testing.T) {
		// The "@g.us" address-space wildcard matches the group chat_id suffix, so a
		// group message is skipped with an error and no info is returned.
		config.ChatwootIgnoreJids = []string{"@g.us"}
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":    "628111@s.whatsapp.net",
			"chat_id": "120363999000111@g.us",
		})
		if err == nil {
			t.Fatalf("expected ignored-JID error, got info=%+v", info)
		}
		if info != nil {
			t.Fatalf("expected nil info on ignore, got %+v", info)
		}
	})

	t.Run("@g.us wildcard does not reject a 1:1 chat", func(t *testing.T) {
		// Same wildcard must leave ordinary @s.whatsapp.net chats untouched — the
		// suffix doesn't match, so normal processing proceeds.
		config.ChatwootIgnoreJids = []string{"@g.us"}
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":    "628123456789@s.whatsapp.net",
			"chat_id": "628123456789@s.whatsapp.net",
		})
		if err != nil {
			t.Fatalf("unexpected error for non-group under @g.us ignore: %v", err)
		}
		if info == nil || info.Identifier != "628123456789" {
			t.Fatalf("expected normal processing, got %+v", info)
		}
	})

	t.Run("exact JID rejects matching from/chat_id", func(t *testing.T) {
		// An exact (non-wildcard) entry rejects precisely that JID. Here it matches
		// both 'from' and 'chat_id'.
		config.ChatwootIgnoreJids = []string{"628123456789@s.whatsapp.net"}
		_, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":    "628123456789@s.whatsapp.net",
			"chat_id": "628123456789@s.whatsapp.net",
		})
		if err == nil {
			t.Fatal("expected exact-JID ignore to reject")
		}
	})

	t.Run("exact JID rejects when only chat_id matches", func(t *testing.T) {
		// The ignore check ORs 'chat_id' and 'from'; a match on chat_id alone is
		// enough to skip, even with a different (allowed) sender.
		config.ChatwootIgnoreJids = []string{"628999@s.whatsapp.net"}
		_, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":    "628000@s.whatsapp.net",
			"chat_id": "628999@s.whatsapp.net",
		})
		if err == nil {
			t.Fatal("expected ignore to trigger on chat_id match alone")
		}
	})

	t.Run("exact JID rejects when only from matches", func(t *testing.T) {
		// Symmetric to the above: a match on 'from' alone also skips.
		config.ChatwootIgnoreJids = []string{"628777@s.whatsapp.net"}
		_, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":    "628777@s.whatsapp.net",
			"chat_id": "628000@s.whatsapp.net",
		})
		if err == nil {
			t.Fatal("expected ignore to trigger on from match alone")
		}
	})

	t.Run("exact JID does not reject a different JID", func(t *testing.T) {
		// A specific ignore entry must NOT over-match: an unrelated 1:1 chat is
		// processed normally.
		config.ChatwootIgnoreJids = []string{"628123456789@s.whatsapp.net"}
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":      "628000111222@s.whatsapp.net",
			"chat_id":   "628000111222@s.whatsapp.net",
			"from_name": "Other",
		})
		if err != nil {
			t.Fatalf("unexpected error for non-ignored JID: %v", err)
		}
		if info == nil || info.Name != "Other" {
			t.Fatalf("expected normal processing, got %+v", info)
		}
	})

	t.Run("empty ignore list processes normally", func(t *testing.T) {
		// With no ignore patterns, the branch is a no-op and a normal 1:1 message is
		// processed (no rejection).
		config.ChatwootIgnoreJids = nil
		info, err := extractChatwootContactInfo(ctx, map[string]any{
			"from":      "628123456789@s.whatsapp.net",
			"chat_id":   "628123456789@s.whatsapp.net",
			"from_name": "Charlie",
		})
		if err != nil {
			t.Fatalf("unexpected error with empty ignore list: %v", err)
		}
		if info == nil || info.Identifier != "628123456789" || info.Name != "Charlie" {
			t.Fatalf("expected normal processing, got %+v", info)
		}
	})
}

// TestBuildChatwootMessageContentMarkdown pins the WA->CW markdown translation in
// the main message-content path. WhatsApp emphasis must be rewritten to GFM so it
// renders in the Chatwoot inbox, and (for groups) the sender prefix must be applied
// AFTER translation so the prefix itself is never mangled by the translator.
func TestBuildChatwootMessageContentMarkdown(t *testing.T) {
	t.Run("bold and italic body translated to GFM", func(t *testing.T) {
		// "*bold* _it_" is WhatsApp markup; the Chatwoot rendering is "**bold** *it*".
		content, atts := buildChatwootMessageContent(map[string]any{
			"body": "*bold* _it_",
		}, false, "")
		if content != "**bold** *it*" {
			t.Fatalf("content = %q", content)
		}
		if len(atts) != 0 {
			t.Fatalf("expected no attachments, got %v", atts)
		}
	})

	t.Run("strikethrough body translated to GFM", func(t *testing.T) {
		// "~del~" -> "~~del~~"; pins the strike pass too.
		content, _ := buildChatwootMessageContent(map[string]any{
			"body": "~del~",
		}, false, "")
		if content != "~~del~~" {
			t.Fatalf("content = %q", content)
		}
	})

	t.Run("plain body without markup is unchanged", func(t *testing.T) {
		// A body with no formatting delimiters short-circuits the translator and is
		// passed through verbatim.
		content, _ := buildChatwootMessageContent(map[string]any{
			"body": "plain text only",
		}, false, "")
		if content != "plain text only" {
			t.Fatalf("content = %q", content)
		}
	})

	t.Run("group sender prefix applied after translation", func(t *testing.T) {
		// The body is translated first ("*hi*" -> "**hi**"), then the sender prefix is
		// prepended — so the prefix sits outside the translated emphasis.
		content, _ := buildChatwootMessageContent(map[string]any{
			"body": "*hi*",
		}, true, "Alice")
		if content != "Alice: **hi**" {
			t.Fatalf("content = %q", content)
		}
	})

	t.Run("group sender prefix with mixed markup", func(t *testing.T) {
		// End-to-end: mixed markup translated, then prefixed with the sender name.
		content, _ := buildChatwootMessageContent(map[string]any{
			"body": "*bold* _it_",
		}, true, "Bob")
		if content != "Bob: **bold** *it*" {
			t.Fatalf("content = %q", content)
		}
	})
}

// TestIsEventWhitelistedForChatwootEditsDeletes pins the ride-along whitelist rule
// for edit/revoke/delete sub-events: when the base "message" event is whitelisted,
// these derived events are mirrored to Chatwoot without operators having to list
// each one. An empty whitelist allows everything; an unrelated whitelist blocks
// them. The global slice is saved/restored so siblings see the default.
func TestIsEventWhitelistedForChatwootEditsDeletes(t *testing.T) {
	original := config.WhatsappWebhookEvents
	defer func() { config.WhatsappWebhookEvents = original }()

	editDeleteEvents := []string{"message.edited", "message.revoked", "message.deleted"}

	t.Run("message whitelist allows edits and deletes to ride along", func(t *testing.T) {
		// "message" in the whitelist must pull in the derived edit/delete events even
		// though they are not themselves listed.
		config.WhatsappWebhookEvents = []string{"message"}
		for _, e := range editDeleteEvents {
			if !isEventWhitelistedForChatwoot(e) {
				t.Errorf("%q should ride along when 'message' is whitelisted", e)
			}
		}
	})

	t.Run("unrelated whitelist blocks edits and deletes", func(t *testing.T) {
		// A whitelist that doesn't cover "message" (and lists none of the sub-events)
		// must block all of them for Chatwoot.
		config.WhatsappWebhookEvents = []string{"call.offer"}
		for _, e := range editDeleteEvents {
			if isEventWhitelistedForChatwoot(e) {
				t.Errorf("%q should be blocked when whitelist is unrelated", e)
			}
		}
	})

	t.Run("empty whitelist allows edits and deletes", func(t *testing.T) {
		// Empty whitelist is allow-all at the Chatwoot gate.
		config.WhatsappWebhookEvents = nil
		for _, e := range editDeleteEvents {
			if !isEventWhitelistedForChatwoot(e) {
				t.Errorf("%q should be allowed when whitelist is empty", e)
			}
		}
	})

	t.Run("explicitly whitelisted sub-event is allowed", func(t *testing.T) {
		// Listing a sub-event by name (without "message") still allows it via the
		// direct isEventWhitelisted match, independent of the ride-along path.
		config.WhatsappWebhookEvents = []string{"message.edited"}
		if !isEventWhitelistedForChatwoot("message.edited") {
			t.Fatal("expected explicitly whitelisted message.edited to be allowed")
		}
		// A sibling sub-event not listed (and no "message") is still blocked.
		if isEventWhitelistedForChatwoot("message.deleted") {
			t.Fatal("expected message.deleted to be blocked when only message.edited is listed")
		}
	})
}
