package whatsapp

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type chatDisplayNameContactStore struct {
	contacts  map[types.JID]types.ContactInfo
	getErr    error
	allErr    error
	getReads  int
	bulkReads int
}

func (s *chatDisplayNameContactStore) GetContact(_ context.Context, jid types.JID) (types.ContactInfo, error) {
	s.getReads++
	if s.getErr != nil {
		return types.ContactInfo{}, s.getErr
	}
	contact, ok := s.contacts[jid.ToNonAD()]
	if !ok {
		return types.ContactInfo{}, nil
	}
	return contact, nil
}

func (s *chatDisplayNameContactStore) GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	s.bulkReads++
	return s.contacts, s.allErr
}

type chatDisplayNameLIDStore struct {
	lid types.JID
	pn  types.JID
}

func (s *chatDisplayNameLIDStore) PutManyLIDMappings(context.Context, []store.LIDMapping) error {
	return nil
}

func (s *chatDisplayNameLIDStore) PutLIDMapping(context.Context, types.JID, types.JID) error {
	return nil
}

func (s *chatDisplayNameLIDStore) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if lid.ToNonAD() == s.lid.ToNonAD() {
		return s.pn, nil
	}
	return types.EmptyJID, nil
}

func (s *chatDisplayNameLIDStore) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	if pn.ToNonAD() == s.pn.ToNonAD() {
		return s.lid, nil
	}
	return types.EmptyJID, nil
}

func (s *chatDisplayNameLIDStore) GetManyLIDsForPNs(_ context.Context, pns []types.JID) (map[types.JID]types.JID, error) {
	result := make(map[types.JID]types.JID)
	for _, pn := range pns {
		if pn.ToNonAD() == s.pn.ToNonAD() {
			result[pn.ToNonAD()] = s.lid.ToNonAD()
		}
	}
	return result, nil
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
	resolver := newChatDisplayNameResolver(contacts, nil)

	for _, storedName := range []string{"", jid.User, jid.String()} {
		if got := resolver.Resolve(ctx, jid.String(), storedName); got != "Saved Alice" {
			t.Fatalf("Resolve(%q, %q) = %q, want synced full name", jid.String(), storedName, got)
		}
	}
	if contacts.getReads != 1 {
		t.Fatalf("GetContact reads = %d, want 1 cached point lookup", contacts.getReads)
	}
	if contacts.bulkReads != 0 {
		t.Fatalf("GetAllContacts reads = %d, want 0 for one chat", contacts.bulkReads)
	}
}

func TestChatDisplayNameResolverResolvesLateLIDPlaceholderThroughPNContact(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	ctx := context.Background()
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	pn := types.NewJID("628123456789", types.DefaultUserServer)
	contacts := &chatDisplayNameContactStore{contacts: map[types.JID]types.ContactInfo{
		pn: {Found: true, FullName: "Saved Alice"},
	}}
	client := &whatsmeow.Client{Store: &store.Device{
		LIDs: &chatDisplayNameLIDStore{lid: lid, pn: pn},
	}}
	resolver := newChatDisplayNameResolver(contacts, client)

	// The chat was persisted before LID->PN mapping existed, so its stored name
	// is the old LID user placeholder. Once the mapping appears, that placeholder
	// must not outrank the synced PN contact name.
	if got := resolver.Resolve(ctx, lid.String(), lid.User); got != "Saved Alice" {
		t.Fatalf("Resolve(%q, %q) = %q, want synced PN contact name", lid.String(), lid.User, got)
	}
	if contacts.getReads != 1 {
		t.Fatalf("GetContact reads = %d, want one PN contact lookup", contacts.getReads)
	}
}

func TestChatDisplayNameResolverPreservesMeaningfulStoredName(t *testing.T) {
	ctx := context.Background()
	jid := types.NewJID("628123456789", types.DefaultUserServer)
	contacts := &chatDisplayNameContactStore{contacts: map[types.JID]types.ContactInfo{
		jid: {Found: true, FullName: "Saved Alice"},
	}}
	resolver := newChatDisplayNameResolver(contacts, nil)

	if got := resolver.Resolve(ctx, jid.String(), "Support Queue"); got != "Support Queue" {
		t.Fatalf("Resolve() = %q, want meaningful stored chat name", got)
	}
	if contacts.getReads != 0 || contacts.bulkReads != 0 {
		t.Fatalf("contact store reads = point:%d bulk:%d, want none for meaningful stored name", contacts.getReads, contacts.bulkReads)
	}
}

func TestChatDisplayNameResolverUsesContactFallbackOrder(t *testing.T) {
	ctx := context.Background()
	pushJID := types.NewJID("628111111111", types.DefaultUserServer)
	businessJID := types.NewJID("628222222222", types.DefaultUserServer)
	phoneJID := types.NewJID("628333333333", types.DefaultUserServer)
	contacts := &chatDisplayNameContactStore{contacts: map[types.JID]types.ContactInfo{
		pushJID:     {Found: true, PushName: "Push Alice", BusinessName: "Alice Shop"},
		businessJID: {Found: true, BusinessName: "Business Bob"},
	}}
	resolver := newChatDisplayNameResolver(contacts, nil)

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
	if contacts.getReads != 1 || contacts.bulkReads != 1 {
		t.Fatalf("contact store reads = point:%d bulk:%d, want one point then one bulk read", contacts.getReads, contacts.bulkReads)
	}
}

func TestChatDisplayNameResolverKeepsSpecialChatSemantics(t *testing.T) {
	ctx := context.Background()
	contacts := &chatDisplayNameContactStore{}
	resolver := newChatDisplayNameResolver(contacts, nil)

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
	if contacts.getReads != 0 || contacts.bulkReads != 0 {
		t.Fatalf("contact store reads = point:%d bulk:%d, want none for special chats", contacts.getReads, contacts.bulkReads)
	}
}

func TestChatDisplayNameResolverContactReadFailureDoesNotBreakChats(t *testing.T) {
	ctx := context.Background()
	jid := types.NewJID("628123456789", types.DefaultUserServer)
	resolver := newChatDisplayNameResolver(&chatDisplayNameContactStore{getErr: errors.New("contact store unavailable")}, nil)

	if got := resolver.Resolve(ctx, jid.String(), jid.User); got != jid.User {
		t.Fatalf("Resolve() = %q, want deterministic phone fallback %q", got, jid.User)
	}
}
