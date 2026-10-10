package whatsapp

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
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

	// Group subjects and newsletter names replace the "Group <id>" /
	// "Newsletter <id>" placeholders. Each list is fetched at most once per
	// resolver, lazily on the first placeholder, and shared per device through
	// a short-lived cache.
	groups            joinedGroupsGetter
	newsletters       subscribedNewslettersGetter
	nameCacheKey      string
	groupNameCache    *chatNameCache
	newsletterCache   *chatNameCache
	groupNames        map[types.JID]string
	newsletterNames   map[types.JID]string
	groupsLoaded      bool
	newslettersLoaded bool
}

// joinedGroupsGetter and subscribedNewslettersGetter are implemented by
// *whatsmeow.Client.
type joinedGroupsGetter interface {
	GetJoinedGroups(context.Context) ([]*types.GroupInfo, error)
}

type subscribedNewslettersGetter interface {
	GetSubscribedNewsletters(context.Context) ([]*types.NewsletterMetadata, error)
}

// NewChatDisplayNameResolver builds a resolver scoped to one response. Contact
// store failures are deliberately non-fatal: callers still receive the existing
// deterministic JID-derived fallback instead of failing the chat API.
func NewChatDisplayNameResolver(_ context.Context, client *whatsmeow.Client) *ChatDisplayNameResolver {
	var contacts chatContactInfoGetter
	if client != nil && client.Store != nil {
		contacts = client.Store.Contacts
	}
	resolver := newChatDisplayNameResolver(contacts, client)
	// Group and newsletter lists need a live session; offline or test clients
	// keep the placeholder.
	if client != nil && client.IsLoggedIn() {
		resolver.groups = client
		resolver.newsletters = client
		if client.Store != nil && client.Store.ID != nil {
			resolver.nameCacheKey = client.Store.ID.ToNonAD().String()
		}
	}
	return resolver
}

func newChatDisplayNameResolver(contacts chatContactInfoGetter, client *whatsmeow.Client) *ChatDisplayNameResolver {
	return &ChatDisplayNameResolver{
		client:          client,
		contacts:        contacts,
		contactCache:    make(map[types.JID]types.ContactInfo),
		groupNameCache:  sharedGroupNameCache,
		newsletterCache: sharedNewsletterNameCache,
	}
}

// Resolve applies the shared chat display-name policy:
//   - preserve meaningful stored chat names (group subjects, newsletter names,
//     or another explicit label),
//   - for empty/number/JID placeholders use synced FullName,
//   - then synced PushName and BusinessName,
//   - finally use the JID-derived label.
//
// status@broadcast keeps its fixed label. A group or newsletter whose stored
// name is empty or the "Group <id>" / "Newsletter <id>" placeholder shows the
// subject from the joined-groups / subscribed-newsletters list instead, as
// "Group <subject>" / "Newsletter <name>", falling back to the placeholder.
// LID identifiers are normalized through the active device mapping before
// contact lookup when possible.
func (r *ChatDisplayNameResolver) Resolve(ctx context.Context, rawJID, storedName string) string {
	if rawJID == "status@broadcast" {
		return "Status"
	}

	originalJID, originalValid := parseChatJID(rawJID)
	jid, validJID := r.normalizeJID(ctx, rawJID)
	if validJID {
		switch jid.Server {
		case types.GroupServer:
			placeholder := "Group " + jid.User
			if hasDisplayName(storedName) && strings.TrimSpace(storedName) != placeholder {
				return storedName
			}
			if name := r.groupName(ctx, jid); name != "" {
				return "Group " + name
			}
			return placeholder
		case types.NewsletterServer:
			placeholder := "Newsletter " + jid.User
			if hasDisplayName(storedName) && strings.TrimSpace(storedName) != placeholder {
				return storedName
			}
			if name := r.newsletterName(ctx, jid); name != "" {
				return "Newsletter " + name
			}
			return placeholder
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

const (
	chatNameCacheTTL     = 5 * time.Minute
	chatNameFetchTimeout = 10 * time.Second
)

func (r *ChatDisplayNameResolver) groupName(ctx context.Context, jid types.JID) string {
	if r == nil || r.groups == nil {
		return ""
	}
	if !r.groupsLoaded {
		r.groupsLoaded = true
		r.groupNames = loadChatNames(ctx, r.groupNameCache, r.nameCacheKey, func(ctx context.Context) (map[types.JID]string, error) {
			groups, err := r.groups.GetJoinedGroups(ctx)
			if err != nil {
				return nil, err
			}
			names := make(map[types.JID]string, len(groups))
			for _, group := range groups {
				if group != nil && hasDisplayName(group.Name) {
					names[group.JID.ToNonAD()] = strings.TrimSpace(group.Name)
				}
			}
			return names, nil
		})
	}
	return r.groupNames[jid.ToNonAD()]
}

func (r *ChatDisplayNameResolver) newsletterName(ctx context.Context, jid types.JID) string {
	if r == nil || r.newsletters == nil {
		return ""
	}
	if !r.newslettersLoaded {
		r.newslettersLoaded = true
		r.newsletterNames = loadChatNames(ctx, r.newsletterCache, r.nameCacheKey, func(ctx context.Context) (map[types.JID]string, error) {
			newsletters, err := r.newsletters.GetSubscribedNewsletters(ctx)
			if err != nil {
				return nil, err
			}
			names := make(map[types.JID]string, len(newsletters))
			for _, newsletter := range newsletters {
				if newsletter != nil && hasDisplayName(newsletter.ThreadMeta.Name.Text) {
					names[newsletter.ID.ToNonAD()] = strings.TrimSpace(newsletter.ThreadMeta.Name.Text)
				}
			}
			return names, nil
		})
	}
	return r.newsletterNames[jid.ToNonAD()]
}

// loadChatNames serves names from the per-device cache, or fetches and caches
// them. Failures are not cached and leave the placeholder in place.
func loadChatNames(ctx context.Context, cache *chatNameCache, key string, fetch func(context.Context) (map[types.JID]string, error)) map[types.JID]string {
	if names, ok := cache.get(key); ok {
		return names
	}
	fetchCtx, cancel := context.WithTimeout(ctx, chatNameFetchTimeout)
	defer cancel()
	names, err := fetch(fetchCtx)
	if err != nil {
		logrus.Debugf("Could not load group/newsletter names for chat display: %v", err)
		return nil
	}
	cache.set(key, names)
	return names
}

// chatNameCache keeps group or newsletter names per device for a few minutes,
// so chat list requests do not query WhatsApp every time.
type chatNameCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]chatNameCacheEntry
}

type chatNameCacheEntry struct {
	names   map[types.JID]string
	expires time.Time
}

var (
	sharedGroupNameCache      = newChatNameCache(chatNameCacheTTL, time.Now)
	sharedNewsletterNameCache = newChatNameCache(chatNameCacheTTL, time.Now)
)

func newChatNameCache(ttl time.Duration, now func() time.Time) *chatNameCache {
	return &chatNameCache{ttl: ttl, now: now, entries: make(map[string]chatNameCacheEntry)}
}

func (c *chatNameCache) get(key string) (map[types.JID]string, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.now().Before(entry.expires) {
		return nil, false
	}
	return entry.names, true
}

func (c *chatNameCache) set(key string, names map[types.JID]string) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = chatNameCacheEntry{names: names, expires: c.now().Add(c.ttl)}
}
