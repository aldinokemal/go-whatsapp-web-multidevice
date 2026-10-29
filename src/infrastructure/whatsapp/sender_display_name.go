package whatsapp

import (
	"context"
	"strings"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// contactInfoGetter is the small subset of the WhatsApp contact store needed
// to resolve a sender label.
type contactInfoGetter interface {
	GetContact(context.Context, types.JID) (types.ContactInfo, error)
}

// PreferredContactDisplayName applies the contact-name precedence shared by
// sender labels. Candidates are returned verbatim; whitespace is only treated
// as unavailable while choosing the next candidate.
func PreferredContactDisplayName(contact types.ContactInfo, livePushName string) string {
	for _, candidate := range []string{
		contact.FullName,
		livePushName,
		contact.PushName,
		contact.BusinessName,
	} {
		if hasDisplayName(candidate) {
			return candidate
		}
	}
	return ""
}

// SenderDisplayNameResolver resolves a stable, human-readable sender label.
type SenderDisplayNameResolver struct {
	contacts          contactInfoGetter
	client            *whatsmeow.Client
	accountJID        types.JID
	deviceDisplayName string
}

// NewSenderDisplayNameResolver creates a resolver for one active WhatsApp
// client. A supplied device display name takes precedence for that account's
// own messages; its stored push name is used only when no explicit name exists.
func NewSenderDisplayNameResolver(client *whatsmeow.Client, deviceDisplayName string) *SenderDisplayNameResolver {
	var contacts contactInfoGetter
	var accountJID types.JID
	if client != nil && client.Store != nil {
		contacts = client.Store.Contacts
		if client.Store.ID != nil {
			accountJID = client.Store.ID.ToNonAD()
		}
		if !hasDisplayName(deviceDisplayName) {
			deviceDisplayName = client.Store.PushName
		}
	}
	return newSenderDisplayNameResolver(contacts, client, accountJID, deviceDisplayName)
}

func newSenderDisplayNameResolver(contacts contactInfoGetter, client *whatsmeow.Client, accountJID types.JID, deviceDisplayName string) *SenderDisplayNameResolver {
	return &SenderDisplayNameResolver{
		contacts:          contacts,
		client:            client,
		accountJID:        accountJID.ToNonAD(),
		deviceDisplayName: deviceDisplayName,
	}
}

// Resolve returns the best available display name for senderJID. It never
// exposes a contact-store failure: the JID's user part (or malformed input)
// remains a deterministic fallback.
func (r *SenderDisplayNameResolver) Resolve(ctx context.Context, senderJID string, isFromMe bool, livePushName string) string {
	jid, validJID := r.normalizeJID(ctx, senderJID)
	activeAccount := isFromMe || (validJID && r.isAccountJID(ctx, jid))

	if activeAccount {
		return r.activeAccountDisplayName(jid, validJID, senderJID)
	}

	var contact types.ContactInfo
	if validJID && r.contacts != nil {
		stored, err := r.contacts.GetContact(ctx, jid)
		if err == nil && stored.Found {
			contact = stored
			if hasDisplayName(contact.FullName) {
				return contact.FullName
			}
		}
	}

	if hasDisplayName(livePushName) {
		return livePushName
	}
	if name := PreferredContactDisplayName(contact, ""); name != "" {
		return name
	}
	if validJID && hasDisplayName(jid.User) {
		return jid.User
	}
	return senderJID
}

func (r *SenderDisplayNameResolver) activeAccountDisplayName(senderJID types.JID, validSenderJID bool, rawSenderJID string) string {
	if hasDisplayName(r.deviceDisplayName) {
		return r.deviceDisplayName
	}
	if hasDisplayName(r.accountJID.User) {
		return r.accountJID.User
	}
	if accountJID := r.accountJID.String(); accountJID != "" {
		return accountJID
	}
	if validSenderJID && hasDisplayName(senderJID.User) {
		return senderJID.User
	}
	if validSenderJID && senderJID.String() != "" {
		return senderJID.String()
	}
	return rawSenderJID
}

func (r *SenderDisplayNameResolver) normalizeJID(ctx context.Context, senderJID string) (types.JID, bool) {
	jid, err := types.ParseJID(senderJID)
	if err != nil || jid.IsEmpty() || !hasDisplayName(jid.User) {
		return types.EmptyJID, false
	}
	jid = jid.ToNonAD()
	if jid.Server == types.HiddenUserServer && r.client != nil && r.client.Store != nil && r.client.Store.LIDs != nil {
		jid = NormalizeJIDFromLID(ctx, jid, r.client).ToNonAD()
	}
	if jid.IsEmpty() || !hasDisplayName(jid.User) {
		return types.EmptyJID, false
	}
	return jid, true
}

func (r *SenderDisplayNameResolver) isAccountJID(ctx context.Context, jid types.JID) bool {
	if r.accountJID.IsEmpty() {
		return false
	}
	accountJID := r.accountJID
	if accountJID.Server == types.HiddenUserServer && r.client != nil && r.client.Store != nil && r.client.Store.LIDs != nil {
		accountJID = NormalizeJIDFromLID(ctx, accountJID, r.client).ToNonAD()
	}
	return jid == accountJID
}

func (r *SenderDisplayNameResolver) cacheKey(ctx context.Context, senderJID string, isFromMe bool) string {
	jid, validJID := r.normalizeJID(ctx, senderJID)
	if isFromMe || (validJID && r.isAccountJID(ctx, jid)) {
		return "self"
	}
	if validJID {
		return jid.String()
	}
	return senderJID
}

func hasDisplayName(value string) bool {
	return strings.TrimSpace(value) != ""
}

func addSenderDisplayName(ctx context.Context, client *whatsmeow.Client, payload map[string]any, isFromMe bool, livePushName string) {
	senderJID, ok := payload["from"].(string)
	if !ok || senderJID == "" {
		return
	}

	resolver := NewSenderDisplayNameResolver(client, "")
	payload["sender_display_name"] = resolver.Resolve(ctx, senderJID, isFromMe, livePushName)
}

// SenderDisplayNameCache memoizes labels for the lifetime of one response or
// webhook payload build.
type SenderDisplayNameCache struct {
	resolver *SenderDisplayNameResolver
	mu       sync.Mutex
	values   map[string]string
}

func NewSenderDisplayNameCache(resolver *SenderDisplayNameResolver) *SenderDisplayNameCache {
	return &SenderDisplayNameCache{
		resolver: resolver,
		values:   make(map[string]string),
	}
}

func (c *SenderDisplayNameCache) Resolve(ctx context.Context, senderJID string, isFromMe bool, livePushName string) string {
	if c == nil || c.resolver == nil {
		return senderJID
	}

	key := c.resolver.cacheKey(ctx, senderJID, isFromMe)
	c.mu.Lock()
	defer c.mu.Unlock()
	if value, ok := c.values[key]; ok {
		return value
	}
	value := c.resolver.Resolve(ctx, senderJID, isFromMe, livePushName)
	c.values[key] = value
	return value
}
