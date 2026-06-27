package pgimport

import (
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
)

// Chatwoot enum values. These have been stable across Chatwoot 2.x → 4.x;
// they come from the Rails models `Message.message_types`, `Message.statuses`,
// `Message.content_types`, and `Conversation.statuses`.
//
// If a future Chatwoot release renumbers these, only this file needs to
// change — every writer function reads the constants from here.
const (
	// message_type enum
	messageTypeIncoming = 0
	messageTypeOutgoing = 1
	// (activity = 2, template = 3 — unused by the importer)

	// message status enum (sent = 0, read = 2, failed = 3 — unused: incoming WA
	// history carries no read receipts, so every imported row is "delivered")
	messageStatusDelivered = 1

	// content_type enum — we only ever write plain text
	contentTypeText = 0

	// conversation status enum
	conversationStatusOpen     = 0
	conversationStatusResolved = 1
	conversationStatusPending  = 2
	// snoozed = 3 (unused by the importer)

	// Rails single-table-inheritance values on messages.sender_type
	senderTypeContact = "Contact"
)

// isGroupJID returns true if the WhatsApp JID represents a group chat.
// Mirrors the check used throughout sync.go so the two paths agree.
func isGroupJID(jid string) bool {
	return strings.HasSuffix(jid, "@g.us")
}

// isLidJID returns true for the privacy-preserving @lid identifiers that
// WhatsApp now uses for some groups and channels. These are not phone
// numbers and must not be normalized to E.164.
func isLidJID(jid string) bool {
	return strings.HasSuffix(jid, "@lid")
}

// contactIdentity computes the (phone_number, identifier, display_name)
// triple that the importer writes to Chatwoot's `contacts` table.
//
// This matches the behavior of the REST client in the parent package
// (client.go:CreateContact) so that contacts created by the live path and
// contacts created by the importer resolve to the same row on lookup by
// `custom_attributes.gowa_whatsapp_jid`.
func contactIdentity(jid, fallbackName string) (phoneNumber, identifier, name string) {
	name = strings.TrimSpace(fallbackName)
	if name == "" {
		name = utils.ExtractPhoneFromJID(jid)
	}

	// Groups and @lid JIDs have no phone number — store JID as identifier.
	if isGroupJID(jid) || isLidJID(jid) {
		return "", jid, name
	}

	// 1:1 chat: phone number in E.164, no Rails identifier.
	return utils.NormalizePhoneE164(jid), "", name
}

// messageTypeForWA returns the Chatwoot message_type enum for a WhatsApp
// message based on its direction. IsFromMe ⇒ outgoing (agent side in
// Chatwoot), otherwise incoming (customer side).
func messageTypeForWA(isFromMe bool) int {
	if isFromMe {
		return messageTypeOutgoing
	}
	return messageTypeIncoming
}
