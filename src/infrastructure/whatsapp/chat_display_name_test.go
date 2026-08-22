package whatsapp

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

type chatDisplayNameContactStore struct {
	contacts map[types.JID]types.ContactInfo
	err      error
	reads    int
}

func (s *chatDisplayNameContactStore) GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	s.reads++
	return s.contacts, s.err
}

func TestChatDisplayNameResolverUsesSyncedContactForFallbackChatName(t *testing.T) {
	ctx := context.Background()
	jid := types.NewJID("628123456789", types.DefaultUserServer)
	contacts := &chatDisplayNameContactStore{contacts: map[types.JID]types.ContactInfo{
		jid: {
			Found:        true,
			FullName:     "Saved Alice",
			PushName:     "Alice WA",
			BusinessName: "Alice Shop",
		},
	}}
	resolver := newChatDisplayNameResolver(ctx, contacts, nil)

	for _, storedName := range []string{"", jid.User, jid.String()} {
		if got := resolver.Resolve(ctx, jid.String(), storedName); got != "Saved Alice" {
			t.Fatalf("Resolve(%q, %q) = %q, want synced full name", jid.String(), storedName, got)
		}
	}
	if contacts.reads != 1 {
		t.Fatalf("GetAllContacts reads = %d, want 1", contacts.reads)
	}
}

func TestChatDisplayNameResolverPreservesMeaningfulStoredName(t *testing.T) {
	ctx := context.Background()
	jid := types.NewJID("628123456789", types.DefaultUserServer)
	contacts := &chatDisplayNameContactStore{contacts: map[types.JID]types.ContactInfo{
		jid: {Found: true, FullName: "Saved Alice"},
	}}
	resolver := newChatDisplayNameResolver(ctx, contacts, nil)

	if got := resolver.Resolve(ctx, jid.String(), "Support Queue"); got != "Support Queue" {
		t.Fatalf("Resolve() = %q, want meaningful stored chat name", got)
	}
}

func TestChatDisplayNameResolverUsesContactFallbackOrder(t *testing.T) {
	ctx := context.Background()
	pushJID := types.NewJID("628111111111", types.DefaultUserServer)
	businessJID := types.NewJID("628222222222", types.DefaultUserServer)
	phoneJID := types.NewJID("628333333333", types.DefaultUserServer)
	resolver := newChatDisplayNameResolver(ctx, &chatDisplayNameContactStore{contacts: map[types.JID]types.ContactInfo{
		pushJID:     {Found: true, PushName: "Push Alice", BusinessName: "Alice Shop"},
		businessJID: {Found: true, BusinessName: "Business Bob"},
	}}, nil)

	cases := []struct {
		name string
		jid  types.JID
		want string
	}{
		{name: "push name", jid: pushJID, want: "Push Alice"},
		{name: "business name", jid: businessJID, want: "Business Bob"},
		{name: "phone number", jid: phoneJID, want: phoneJID.User},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolver.Resolve(ctx, tc.jid.String(), tc.jid.User); got != tc.want {
				t.Fatalf("Resolve() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChatDisplayNameResolverKeepsSpecialChatSemantics(t *testing.T) {
	ctx := context.Background()
	resolver := newChatDisplayNameResolver(ctx, &chatDisplayNameContactStore{}, nil)

	cases := []struct {
		jid        string
		storedName string
		want       string
	}{
		{jid: "status@broadcast", want: "Status"},
		{jid: "120363999000111@g.us", storedName: "Family", want: "Family"},
		{jid: "120363999000111@g.us", want: "Group 120363999000111"},
		{jid: "120363111@newsletter", storedName: "Updates", want: "Updates"},
		{jid: "120363111@newsletter", want: "Newsletter 120363111"},
	}
	for _, tc := range cases {
		if got := resolver.Resolve(ctx, tc.jid, tc.storedName); got != tc.want {
			t.Fatalf("Resolve(%q, %q) = %q, want %q", tc.jid, tc.storedName, got, tc.want)
		}
	}
}

func TestChatDisplayNameResolverContactReadFailureDoesNotBreakChats(t *testing.T) {
	ctx := context.Background()
	jid := types.NewJID("628123456789", types.DefaultUserServer)
	resolver := newChatDisplayNameResolver(ctx, &chatDisplayNameContactStore{err: errors.New("contact store unavailable")}, nil)

	if got := resolver.Resolve(ctx, jid.String(), jid.User); got != jid.User {
		t.Fatalf("Resolve() = %q, want deterministic phone fallback %q", got, jid.User)
	}
}
