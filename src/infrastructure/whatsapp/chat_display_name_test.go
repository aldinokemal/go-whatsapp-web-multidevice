package whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

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

type fakeChatNameSource struct {
	groups      []*types.GroupInfo
	newsletters []*types.NewsletterMetadata
	err         error
	groupReads  int
	letterReads int
}

func (s *fakeChatNameSource) GetJoinedGroups(context.Context) ([]*types.GroupInfo, error) {
	s.groupReads++
	return s.groups, s.err
}

func (s *fakeChatNameSource) GetSubscribedNewsletters(context.Context) ([]*types.NewsletterMetadata, error) {
	s.letterReads++
	return s.newsletters, s.err
}

func newTestChatNameSource() *fakeChatNameSource {
	group := &types.GroupInfo{JID: types.NewJID("120363000000000001", types.GroupServer)}
	group.Name = "Family"
	other := &types.GroupInfo{JID: types.NewJID("120363000000000002", types.GroupServer)}
	other.Name = "Work"
	newsletter := &types.NewsletterMetadata{ID: types.NewJID("120363000000000009", types.NewsletterServer)}
	newsletter.ThreadMeta.Name.Text = "Tech Channel"
	return &fakeChatNameSource{
		groups:      []*types.GroupInfo{group, other},
		newsletters: []*types.NewsletterMetadata{newsletter},
	}
}

func newTestChatNameResolver(source *fakeChatNameSource, cacheKey string, groupCache, newsletterCache *chatNameCache) *ChatDisplayNameResolver {
	resolver := newChatDisplayNameResolver(nil, nil)
	resolver.groups = source
	resolver.newsletters = source
	resolver.nameCacheKey = cacheKey
	resolver.groupNameCache = groupCache
	resolver.newsletterCache = newsletterCache
	return resolver
}

func TestChatDisplayNameResolverReplacesGroupAndNewsletterPlaceholders(t *testing.T) {
	ctx := context.Background()
	source := newTestChatNameSource()
	resolver := newTestChatNameResolver(source, "", nil, nil)

	tests := []struct {
		jid, stored, want string
	}{
		{"120363000000000001@g.us", "Group 120363000000000001", "Group Family"},
		{"120363000000000002@g.us", "", "Group Work"},
		{"120363000000000003@g.us", "Group 120363000000000003", "Group 120363000000000003"},
		{"120363000000000001@g.us", "Family (stored)", "Family (stored)"},
		{"120363000000000009@newsletter", "Newsletter 120363000000000009", "Newsletter Tech Channel"},
		{"120363000000000008@newsletter", "", "Newsletter 120363000000000008"},
	}
	for _, tt := range tests {
		if got := resolver.Resolve(ctx, tt.jid, tt.stored); got != tt.want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", tt.jid, tt.stored, got, tt.want)
		}
	}
	if source.groupReads != 1 || source.letterReads != 1 {
		t.Fatalf("expected one group and one newsletter list read per resolver, got %d and %d", source.groupReads, source.letterReads)
	}
}

func TestChatDisplayNameResolverSkipsListReadsForStoredNames(t *testing.T) {
	source := newTestChatNameSource()
	resolver := newTestChatNameResolver(source, "", nil, nil)

	if got := resolver.Resolve(context.Background(), "120363000000000001@g.us", "Family"); got != "Family" {
		t.Fatalf("expected stored group name to be kept, got %q", got)
	}
	if got := resolver.Resolve(context.Background(), "628123456789@s.whatsapp.net", "Alice"); got != "Alice" {
		t.Fatalf("expected stored contact name to be kept, got %q", got)
	}
	if source.groupReads != 0 || source.letterReads != 0 {
		t.Fatalf("expected no list reads, got %d groups and %d newsletters", source.groupReads, source.letterReads)
	}
}

func TestChatDisplayNameResolverFallsBackWhenListReadFails(t *testing.T) {
	source := &fakeChatNameSource{err: errors.New("not connected")}
	resolver := newTestChatNameResolver(source, "", nil, nil)

	for range 2 {
		if got := resolver.Resolve(context.Background(), "120363000000000001@g.us", ""); got != "Group 120363000000000001" {
			t.Fatalf("expected placeholder on failure, got %q", got)
		}
	}
	if source.groupReads != 1 {
		t.Fatalf("expected a failed read to be attempted once per resolver, got %d", source.groupReads)
	}
}

func TestChatDisplayNameResolverCachesNamesPerDevice(t *testing.T) {
	now := time.Date(2026, time.October, 10, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	groupCache := newChatNameCache(time.Minute, clock)
	newsletterCache := newChatNameCache(time.Minute, clock)
	source := newTestChatNameSource()
	resolve := func(key string) string {
		resolver := newTestChatNameResolver(source, key, groupCache, newsletterCache)
		return resolver.Resolve(context.Background(), "120363000000000001@g.us", "")
	}

	if got := resolve("device-a@s.whatsapp.net"); got != "Group Family" {
		t.Fatalf("unexpected name %q", got)
	}
	resolve("device-a@s.whatsapp.net")
	if source.groupReads != 1 {
		t.Fatalf("expected cached names to be reused across resolvers, got %d reads", source.groupReads)
	}

	resolve("device-b@s.whatsapp.net")
	if source.groupReads != 2 {
		t.Fatalf("expected a separate cache entry per device, got %d reads", source.groupReads)
	}

	now = now.Add(time.Minute)
	resolve("device-a@s.whatsapp.net")
	if source.groupReads != 3 {
		t.Fatalf("expected names to be read again after the TTL, got %d reads", source.groupReads)
	}
}
