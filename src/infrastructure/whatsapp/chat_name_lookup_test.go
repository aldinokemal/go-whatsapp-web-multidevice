package whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow/types"
)

var (
	testGroupJID      = types.NewJID("120363012345678901", types.GroupServer)
	testNewsletterJID = types.NewJID("120363111111111111", types.NewsletterServer)
	testContactJID    = types.NewJID("628123456789", types.DefaultUserServer)
)

func TestPlaceholderChatName(t *testing.T) {
	tests := []struct {
		jid  types.JID
		want string
	}{
		{testGroupJID, "Group 120363012345678901"},
		{testNewsletterJID, "Newsletter 120363111111111111"},
		{testContactJID, ""},
	}
	for _, tt := range tests {
		if got := PlaceholderChatName(tt.jid); got != tt.want {
			t.Errorf("PlaceholderChatName(%s) = %q, want %q", tt.jid, got, tt.want)
		}
	}
}

func TestIsPlaceholderChatName(t *testing.T) {
	tests := []struct {
		name   string
		jid    types.JID
		stored string
		want   bool
	}{
		{"empty group name", testGroupJID, "", true},
		{"group placeholder", testGroupJID, "Group 120363012345678901", true},
		{"resolved group name", testGroupJID, "Group: Family", false},
		{"legacy unprefixed group name", testGroupJID, "Family", false},
		{"newsletter placeholder", testNewsletterJID, "Newsletter 120363111111111111", true},
		{"resolved newsletter name", testNewsletterJID, "Newsletter: Tech Channel", false},
		{"contact is never a placeholder", testContactJID, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsPlaceholderChatName(tt.jid, tt.stored); got != tt.want {
				t.Fatalf("IsPlaceholderChatName(%s, %q) = %v, want %v", tt.jid, tt.stored, got, tt.want)
			}
		})
	}
}

func TestFormatChatName(t *testing.T) {
	tests := []struct {
		name    string
		jid     types.JID
		subject string
		want    string
	}{
		{"group", testGroupJID, "Family", "Group: Family"},
		{"group subject is trimmed", testGroupJID, "  Family  ", "Group: Family"},
		{"blank group subject", testGroupJID, "   ", ""},
		{"newsletter", testNewsletterJID, "Tech Channel", "Newsletter: Tech Channel"},
		{"contact", testContactJID, "Alice", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatChatName(tt.jid, tt.subject); got != tt.want {
				t.Fatalf("FormatChatName(%s, %q) = %q, want %q", tt.jid, tt.subject, got, tt.want)
			}
		})
	}
}

func TestResolvePlaceholderChatNameWithoutClientKeepsName(t *testing.T) {
	placeholder := PlaceholderChatName(testGroupJID)
	if got := ResolvePlaceholderChatName(context.Background(), nil, testGroupJID, placeholder); got != placeholder {
		t.Fatalf("expected placeholder to be kept without a client, got %q", got)
	}
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestChatNameLookupThrottleResolvesWithPrefix(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, time.October, 10, 9, 0, 0, 0, time.UTC)}
	throttle := newChatNameLookupThrottle(clock.Now)

	calls := 0
	fetch := func(context.Context, types.JID) (string, error) {
		calls++
		return "Family", nil
	}

	name, ok := throttle.resolve(context.Background(), "dev|group", testGroupJID, fetch)
	if !ok || name != "Group: Family" {
		t.Fatalf("resolve = (%q, %v), want (%q, true)", name, ok, "Group: Family")
	}
	if len(throttle.entries) != 0 {
		t.Fatalf("expected a successful lookup to clear its throttle entry, got %d entries", len(throttle.entries))
	}

	name, ok = throttle.resolve(context.Background(), "dev|newsletter", testNewsletterJID, func(context.Context, types.JID) (string, error) {
		return "Tech Channel", nil
	})
	if !ok || name != "Newsletter: Tech Channel" {
		t.Fatalf("resolve = (%q, %v), want (%q, true)", name, ok, "Newsletter: Tech Channel")
	}
	if calls != 1 {
		t.Fatalf("expected one group lookup, got %d", calls)
	}
}

func TestChatNameLookupThrottleBacksOffAfterFailure(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, time.October, 10, 9, 0, 0, 0, time.UTC)}
	throttle := newChatNameLookupThrottle(clock.Now)

	calls := 0
	failing := func(context.Context, types.JID) (string, error) {
		calls++
		return "", errors.New("not connected")
	}

	if _, ok := throttle.resolve(context.Background(), "dev|group", testGroupJID, failing); ok {
		t.Fatal("expected failed lookup to report !ok")
	}
	if calls != 1 {
		t.Fatalf("expected 1 lookup, got %d", calls)
	}

	// Within the backoff window, further messages must not trigger a lookup.
	clock.now = clock.now.Add(chatNameLookupBaseBackoff - time.Second)
	if _, ok := throttle.resolve(context.Background(), "dev|group", testGroupJID, failing); ok {
		t.Fatal("expected throttled lookup to report !ok")
	}
	if calls != 1 {
		t.Fatalf("expected lookup to be throttled, got %d calls", calls)
	}

	// After the window it retries, and the next window doubles.
	clock.now = clock.now.Add(2 * time.Second)
	throttle.resolve(context.Background(), "dev|group", testGroupJID, failing)
	if calls != 2 {
		t.Fatalf("expected retry after backoff, got %d calls", calls)
	}
	if got := throttle.entries["dev|group"].backoff; got != 2*chatNameLookupBaseBackoff {
		t.Fatalf("expected backoff to double to %s, got %s", 2*chatNameLookupBaseBackoff, got)
	}

	// A blank subject counts as a failure too.
	clock.now = clock.now.Add(chatNameLookupMaxBackoff)
	if _, ok := throttle.resolve(context.Background(), "dev|group", testGroupJID, func(context.Context, types.JID) (string, error) {
		return "  ", nil
	}); ok {
		t.Fatal("expected blank subject to report !ok")
	}

	// Other chats are not affected by this chat's backoff.
	if _, ok := throttle.resolve(context.Background(), "dev|other", testGroupJID, func(context.Context, types.JID) (string, error) {
		return "Other", nil
	}); !ok {
		t.Fatal("expected lookup for a different chat to run")
	}
}

func TestChatNameLookupThrottleCapsBackoff(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, time.October, 10, 9, 0, 0, 0, time.UTC)}
	throttle := newChatNameLookupThrottle(clock.Now)

	for i := 0; i < 20; i++ {
		throttle.fail("dev|group")
	}
	if got := throttle.entries["dev|group"].backoff; got != chatNameLookupMaxBackoff {
		t.Fatalf("expected backoff capped at %s, got %s", chatNameLookupMaxBackoff, got)
	}
}

func TestChatNameLookupThrottleSkipsWhileInFlight(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, time.October, 10, 9, 0, 0, 0, time.UTC)}
	throttle := newChatNameLookupThrottle(clock.Now)

	if !throttle.begin("dev|group") {
		t.Fatal("expected first lookup to start")
	}
	if throttle.begin("dev|group") {
		t.Fatal("expected concurrent lookup for the same chat to be skipped")
	}
}

type chatNameStoreSpy struct {
	chat   *domainChatStorage.Chat
	stored *domainChatStorage.Chat
}

func (s *chatNameStoreSpy) GetChatByDevice(_, _ string) (*domainChatStorage.Chat, error) {
	return s.chat, nil
}

func (s *chatNameStoreSpy) StoreChat(chat *domainChatStorage.Chat) error {
	s.stored = chat
	return nil
}

func TestUpdateGroupChatNameStoresPrefixedSubject(t *testing.T) {
	lastMessage := time.Date(2026, time.October, 10, 8, 0, 0, 0, time.UTC)
	store := &chatNameStoreSpy{chat: &domainChatStorage.Chat{
		DeviceID:        "device-a@s.whatsapp.net",
		JID:             testGroupJID.String(),
		Name:            "Group: Old Name",
		LastMessageTime: lastMessage,
		Archived:        true,
	}}

	updateGroupChatName(store, "device-a@s.whatsapp.net", testGroupJID, "New Name")

	if store.stored == nil {
		t.Fatal("expected chat to be stored")
	}
	if store.stored.Name != "Group: New Name" {
		t.Fatalf("stored name = %q, want %q", store.stored.Name, "Group: New Name")
	}
	if !store.stored.LastMessageTime.Equal(lastMessage) || !store.stored.Archived {
		t.Fatalf("expected other chat fields to be preserved, got %+v", store.stored)
	}
}

func TestUpdateGroupChatNameSkipsMissingChatAndBlankSubject(t *testing.T) {
	store := &chatNameStoreSpy{}
	updateGroupChatName(store, "device-a@s.whatsapp.net", testGroupJID, "New Name")
	if store.stored != nil {
		t.Fatal("expected no chat to be created for an unknown group")
	}

	store = &chatNameStoreSpy{chat: &domainChatStorage.Chat{JID: testGroupJID.String(), Name: "Group: Family"}}
	updateGroupChatName(store, "device-a@s.whatsapp.net", testGroupJID, "  ")
	if store.stored != nil {
		t.Fatal("expected a blank subject to leave the chat untouched")
	}
}
