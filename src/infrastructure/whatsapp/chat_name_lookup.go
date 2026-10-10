package whatsapp

import (
	"context"
	"strings"
	"sync"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// Stored names for groups and newsletters carry a prefix so they can be told
// apart from contacts in the chat list.
const (
	GroupChatNamePrefix      = "Group: "
	NewsletterChatNamePrefix = "Newsletter: "
)

const (
	chatNameLookupTimeout     = 5 * time.Second
	chatNameLookupBaseBackoff = 30 * time.Second
	chatNameLookupMaxBackoff  = 30 * time.Minute
)

// PlaceholderChatName returns the generic name used for a group or newsletter
// chat whose real name is not known yet ("Group <id>" / "Newsletter <id>").
// It returns "" for any other kind of chat.
func PlaceholderChatName(jid types.JID) string {
	switch jid.Server {
	case types.GroupServer:
		return "Group " + jid.User
	case types.NewsletterServer:
		return "Newsletter " + jid.User
	default:
		return ""
	}
}

// IsPlaceholderChatName reports whether name is empty or still the generic
// placeholder for a group or newsletter chat. It is always false for other chats.
func IsPlaceholderChatName(jid types.JID, name string) bool {
	placeholder := PlaceholderChatName(jid)
	if placeholder == "" {
		return false
	}
	name = strings.TrimSpace(name)
	return name == "" || name == placeholder
}

// FormatChatName builds the stored name for a group subject or newsletter name:
// "Group: <subject>" or "Newsletter: <name>". It returns "" when the subject is
// blank or the chat is neither a group nor a newsletter.
func FormatChatName(jid types.JID, subject string) string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return ""
	}
	switch jid.Server {
	case types.GroupServer:
		return GroupChatNamePrefix + subject
	case types.NewsletterServer:
		return NewsletterChatNamePrefix + subject
	default:
		return ""
	}
}

// ResolvePlaceholderChatName replaces a group or newsletter placeholder name
// with the real, prefixed name looked up through the client. Any other name is
// returned unchanged, so the network lookup only happens while the stored name
// is still a placeholder. When the lookup fails or is throttled, name is
// returned as is and the lookup is retried on a later call.
func ResolvePlaceholderChatName(ctx context.Context, client *whatsmeow.Client, jid types.JID, name string) string {
	if client == nil || !IsPlaceholderChatName(jid, name) {
		return name
	}
	fetch := func(ctx context.Context, jid types.JID) (string, error) {
		return fetchChatSubject(ctx, client, jid)
	}
	if resolved, ok := chatNameLookups.resolve(ctx, chatNameLookupKey(client, jid), jid, fetch); ok {
		return resolved
	}
	return name
}

func fetchChatSubject(ctx context.Context, client *whatsmeow.Client, jid types.JID) (string, error) {
	switch jid.Server {
	case types.GroupServer:
		info, err := client.GetGroupInfo(ctx, jid)
		if err != nil || info == nil {
			return "", err
		}
		return info.Name, nil
	case types.NewsletterServer:
		meta, err := client.GetNewsletterInfo(ctx, jid)
		if err != nil || meta == nil {
			return "", err
		}
		return meta.ThreadMeta.Name.Text, nil
	default:
		return "", nil
	}
}

func chatNameLookupKey(client *whatsmeow.Client, jid types.JID) string {
	device := ""
	if client != nil && client.Store != nil && client.Store.ID != nil {
		device = client.Store.ID.ToNonAD().String()
	}
	return device + "|" + jid.ToNonAD().String()
}

// chatNameLookupThrottle keeps a small in-memory, per-chat backoff so a failing
// lookup is not repeated for every incoming message of that chat.
type chatNameLookupThrottle struct {
	mu      sync.Mutex
	entries map[string]*chatNameLookupState
	now     func() time.Time
}

type chatNameLookupState struct {
	nextAttempt time.Time
	backoff     time.Duration
}

var chatNameLookups = newChatNameLookupThrottle(time.Now)

func newChatNameLookupThrottle(now func() time.Time) *chatNameLookupThrottle {
	return &chatNameLookupThrottle{
		entries: make(map[string]*chatNameLookupState),
		now:     now,
	}
}

// begin reports whether a lookup for key may run now. A granted lookup holds
// the slot for its timeout so concurrent messages of the same chat skip it.
func (t *chatNameLookupThrottle) begin(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.now()
	state, ok := t.entries[key]
	if !ok {
		state = &chatNameLookupState{}
		t.entries[key] = state
	}
	if now.Before(state.nextAttempt) {
		return false
	}
	state.nextAttempt = now.Add(chatNameLookupTimeout)
	return true
}

func (t *chatNameLookupThrottle) succeed(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, key)
}

func (t *chatNameLookupThrottle) fail(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.entries[key]
	if !ok {
		state = &chatNameLookupState{}
		t.entries[key] = state
	}
	if state.backoff == 0 {
		state.backoff = chatNameLookupBaseBackoff
	} else {
		state.backoff *= 2
		if state.backoff > chatNameLookupMaxBackoff {
			state.backoff = chatNameLookupMaxBackoff
		}
	}
	state.nextAttempt = t.now().Add(state.backoff)
}

func (t *chatNameLookupThrottle) resolve(ctx context.Context, key string, jid types.JID, fetch func(context.Context, types.JID) (string, error)) (string, bool) {
	if !t.begin(key) {
		return "", false
	}

	lookupCtx, cancel := context.WithTimeout(ctx, chatNameLookupTimeout)
	defer cancel()

	subject, err := fetch(lookupCtx, jid)
	name := FormatChatName(jid, subject)
	if err != nil || name == "" {
		t.fail(key)
		logrus.Debugf("Could not resolve chat name for %s, keeping placeholder: %v", jid.String(), err)
		return "", false
	}

	t.succeed(key)
	return name, true
}

// chatNameStore is the part of the chat storage used to rename a stored chat.
type chatNameStore interface {
	GetChatByDevice(deviceID, jid string) (*domainChatStorage.Chat, error)
	StoreChat(chat *domainChatStorage.Chat) error
}

// chatStorageDeviceID returns the device_id used for chat rows: the instance JID
// the event handler passes in, falling back to the logged-in client identity.
func chatStorageDeviceID(deviceID string, client *whatsmeow.Client) string {
	if deviceID != "" {
		return deviceID
	}
	if client != nil && client.Store != nil && client.Store.ID != nil {
		return client.Store.ID.ToNonAD().String()
	}
	return ""
}

// updateGroupChatName stores a changed group subject as the chat name for an
// existing chat of this device. Chats that are not stored yet are left alone;
// they get their name when their first message is stored.
func updateGroupChatName(chatStorageRepo chatNameStore, deviceID string, groupJID types.JID, subject string) {
	if chatStorageRepo == nil || deviceID == "" {
		return
	}
	name := FormatChatName(groupJID, subject)
	if name == "" {
		return
	}

	chatJID := groupJID.ToNonAD().String()
	chat, err := chatStorageRepo.GetChatByDevice(deviceID, chatJID)
	if err != nil {
		logrus.Warnf("Failed to load chat %s to update its name: %v", chatJID, err)
		return
	}
	if chat == nil || chat.Name == name {
		return
	}

	chat.Name = name
	if err := chatStorageRepo.StoreChat(chat); err != nil {
		logrus.Warnf("Failed to update name of chat %s: %v", chatJID, err)
	}
}
