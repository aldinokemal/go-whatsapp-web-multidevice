package whatsapp

import (
	"context"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// allContactInfoGetter is the small subset of the WhatsApp contact store needed
// to resolve chat labels without issuing one contact lookup per chat.
type allContactInfoGetter interface {
	GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error)
}

// ChatDisplayNameResolver resolves a human-readable chat label from the chat
// metadata already stored by GOWA and the address book synced by whatsmeow.
// Contacts are snapshotted once when the resolver is created so list endpoints
// do not turn into N+1 database reads.
type ChatDisplayNameResolver struct {
	client   *whatsmeow.Client
	contacts map[types.JID]types.ContactInfo
}

// NewChatDisplayNameResolver builds a resolver for one response. A contact-store
// failure is deliberately non-fatal: callers still receive the existing
// deterministic JID-derived fallback instead of failing the chat API.
func NewChatDisplayNameResolver(ctx context.Context, client *whatsmeow.Client) *ChatDisplayNameResolver {
	var contacts allContactInfoGetter
	if client != nil && client.Store != nil {
		contacts = client.Store.Contacts
	}
	return newChatDisplayNameResolver(ctx, contacts, client)
}

func newChatDisplayNameResolver(ctx context.Context, contacts allContactInfoGetter, client *whatsmeow.Client) *ChatDisplayNameResolver {
	resolver := &ChatDisplayNameResolver{client: client}
	if contacts == nil {
		return resolver
	}

	allContacts, err := contacts.GetAllContacts(ctx)
	if err == nil {
		resolver.contacts = allContacts
	}
	return resolver
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

	if hasDisplayName(storedName) && !isJIDFallbackName(rawJID, jid, validJID, storedName) {
		return storedName
	}

	if validJID {
		if contact, ok := r.contactInfo(jid); ok {
			if name := PreferredContactDisplayName(contact, ""); name != "" {
				return name
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

func (r *ChatDisplayNameResolver) normalizeJID(ctx context.Context, rawJID string) (types.JID, bool) {
	jid, err := types.ParseJID(rawJID)
	if err != nil || jid.IsEmpty() || !hasDisplayName(jid.User) {
		return types.EmptyJID, false
	}
	jid = jid.ToNonAD()
	if jid.Server == types.HiddenUserServer && r != nil && r.client != nil && r.client.Store != nil && r.client.Store.LIDs != nil {
		jid = NormalizeJIDFromLID(ctx, jid, r.client).ToNonAD()
	}
	if jid.IsEmpty() || !hasDisplayName(jid.User) {
		return types.EmptyJID, false
	}
	return jid, true
}

func (r *ChatDisplayNameResolver) contactInfo(jid types.JID) (types.ContactInfo, bool) {
	if r == nil || r.contacts == nil {
		return types.ContactInfo{}, false
	}
	contact, ok := r.contacts[jid.ToNonAD()]
	if !ok {
		return types.ContactInfo{}, false
	}
	return contact, contact.Found || hasDisplayName(contact.FullName) || hasDisplayName(contact.PushName) || hasDisplayName(contact.BusinessName)
}

func isJIDFallbackName(rawJID string, jid types.JID, validJID bool, storedName string) bool {
	name := strings.TrimSpace(storedName)
	if name == "" {
		return true
	}
	if name == strings.TrimSpace(rawJID) {
		return true
	}
	if !validJID {
		return false
	}
	return name == jid.User || name == jid.String()
}
