package whatsapp

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// chatContactInfoGetter is the subset of the WhatsApp contact store needed to
// resolve chat labels. GetContact keeps single-chat requests cheap, while
// GetAllContacts lets list requests collapse repeated lookups into one snapshot.
type chatContactInfoGetter interface {
	GetContact(context.Context, types.JID) (types.ContactInfo, error)
	GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error)
}

// ChatDisplayNameResolver resolves a human-readable chat label from the chat
// metadata already stored by GOWA and the address book synced by whatsmeow.
// It starts with point lookups, then switches to a contact snapshot when a
// second distinct contact is needed. This keeps chat detail requests O(1)
// without turning chat lists into N+1 database reads.
type ChatDisplayNameResolver struct {
	client        *whatsmeow.Client
	contacts      chatContactInfoGetter
	contactCache  map[types.JID]types.ContactInfo
	allContacts   map[types.JID]types.ContactInfo
	pointLookups  int
	bulkAttempted bool
}

// NewChatDisplayNameResolver builds a resolver scoped to one response. Contact
// store failures are deliberately non-fatal: callers still receive the existing
// deterministic JID-derived fallback instead of failing the chat API.
func NewChatDisplayNameResolver(_ context.Context, client *whatsmeow.Client) *ChatDisplayNameResolver {
	var contacts chatContactInfoGetter
	if client != nil && client.Store != nil {
		contacts = client.Store.Contacts
	}
	return newChatDisplayNameResolver(contacts, client)
}

func newChatDisplayNameResolver(contacts chatContactInfoGetter, client *whatsmeow.Client) *ChatDisplayNameResolver {
	return &ChatDisplayNameResolver{
		client:       client,
		contacts:     contacts,
		contactCache: make(map[types.JID]types.ContactInfo),
	}
}

// Resolve applies the shared chat display-name policy:
//   - preserve meaningful stored chat names (group subjects, newsletter names,
//     or another explicit label),
//   - for empty/number/JID placeholders use synced FullName,
//   - then synced PushName and BusinessName,
//   - finally use the JID-derived label.
//
// status@broadcast, groups, and newsletters retain their existing GOWA fallback
// semantics. LID identifiers are normalized through the active device mapping
// before contact lookup when possible.
func (r *ChatDisplayNameResolver) Resolve(ctx context.Context, rawJID, storedName string) string {
	if rawJID == "status@broadcast" {
		return "Status"
	}

	originalJID, originalValid := parseChatJID(rawJID)
	jid, validJID := r.normalizeJID(ctx, rawJID)
	if validJID {
		switch jid.Server {
		case types.GroupServer:
			if hasDisplayName(storedName) {
				return storedName
			}
			return "Group " + jid.User
		case types.NewsletterServer:
			if hasDisplayName(storedName) {
				return storedName
			}
			return "Newsletter " + jid.User
		}
	}

	if hasDisplayName(storedName) && !isJIDFallbackName(rawJID, originalJID, originalValid, jid, validJID, storedName) {
		return storedName
	}

	if validJID {
		if contact, ok := r.contactInfo(ctx, jid); ok {
			if name := PreferredContactDisplayName(contact, ""); name != "" {
				return name
			}
		}
		if hasDisplayName(jid.User) {
			return jid.User
		}
	}

	if hasDisplayName(storedName) {
		return storedName
	}
	return rawJID
}

func parseChatJID(rawJID string) (types.JID, bool) {
	jid, err := types.ParseJID(rawJID)
	if err != nil || jid.IsEmpty() || !hasDisplayName(jid.User) {
		return types.EmptyJID, false
	}
	return jid.ToNonAD(), true
}

func (r *ChatDisplayNameResolver) normalizeJID(ctx context.Context, rawJID string) (types.JID, bool) {
	jid, valid := parseChatJID(rawJID)
	if !valid {
		return types.EmptyJID, false
	}
	if jid.Server == types.HiddenUserServer && r != nil && r.client != nil && r.client.Store != nil && r.client.Store.LIDs != nil {
		jid = NormalizeJIDFromLID(ctx, jid, r.client).ToNonAD()
	}
	if jid.IsEmpty() || !hasDisplayName(jid.User) {
		return types.EmptyJID, false
	}
	return jid, true
}

func (r *ChatDisplayNameResolver) contactInfo(ctx context.Context, jid types.JID) (types.ContactInfo, bool) {
	if r == nil || r.contacts == nil {
		return types.ContactInfo{}, false
	}
	jid = jid.ToNonAD()

	if contact, ok := r.contactCache[jid]; ok {
		return contact, usableContactInfo(contact)
	}
	if r.allContacts != nil {
		contact, ok := r.allContacts[jid]
		if !ok {
			r.contactCache[jid] = types.ContactInfo{}
			return types.ContactInfo{}, false
		}
		r.contactCache[jid] = contact
		return contact, usableContactInfo(contact)
	}

	// A single chat detail only needs one point lookup. When a response asks for
	// another distinct contact (typical for chat lists), switch to one bulk read
	// and serve the rest from memory.
	if r.pointLookups > 0 && !r.bulkAttempted {
		r.bulkAttempted = true
		if allContacts, err := r.contacts.GetAllContacts(ctx); err == nil {
			r.allContacts = allContacts
			if contact, ok := allContacts[jid]; ok {
				r.contactCache[jid] = contact
				return contact, usableContactInfo(contact)
			}
			r.contactCache[jid] = types.ContactInfo{}
			return types.ContactInfo{}, false
		}
	}

	contact, err := r.contacts.GetContact(ctx, jid)
	r.pointLookups++
	if err != nil {
		return types.ContactInfo{}, false
	}
	r.contactCache[jid] = contact
	return contact, usableContactInfo(contact)
}

func usableContactInfo(contact types.ContactInfo) bool {
	return contact.Found || hasDisplayName(contact.FullName) || hasDisplayName(contact.PushName) || hasDisplayName(contact.BusinessName)
}

func isJIDFallbackName(rawJID string, originalJID types.JID, originalValid bool, normalizedJID types.JID, normalizedValid bool, storedName string) bool {
	name := strings.TrimSpace(storedName)
	if name == "" || name == strings.TrimSpace(rawJID) {
		return true
	}
	if originalValid && (name == originalJID.User || name == originalJID.String()) {
		return true
	}
	if normalizedValid && (name == normalizedJID.User || name == normalizedJID.String()) {
		return true
	}
	return false
}
