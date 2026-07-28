package whatsapp

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

type senderDisplayNameContactGetter struct {
	contacts map[types.JID]types.ContactInfo
	err      error
	calls    int
}

func (g *senderDisplayNameContactGetter) GetContact(_ context.Context, jid types.JID) (types.ContactInfo, error) {
	g.calls++
	if g.err != nil {
		return types.ContactInfo{}, g.err
	}
	return g.contacts[jid], nil
}

func TestPreferredContactDisplayName(t *testing.T) {
	tests := []struct {
		name    string
		contact types.ContactInfo
		live    string
		want    string
	}{
		{
			name:    "saved full name wins",
			contact: types.ContactInfo{FullName: "Saved Name", PushName: "Stored Push", BusinessName: "Business Name"},
			live:    "Live Push",
			want:    "Saved Name",
		},
		{
			name:    "live push name beats stored push name",
			contact: types.ContactInfo{PushName: "Stored Push", BusinessName: "Business Name"},
			live:    "Live Push",
			want:    "Live Push",
		},
		{
			name:    "stored push name beats business name",
			contact: types.ContactInfo{PushName: "Stored Push", BusinessName: "Business Name"},
			want:    "Stored Push",
		},
		{
			name:    "business name is the final contact candidate",
			contact: types.ContactInfo{BusinessName: "Business Name"},
			want:    "Business Name",
		},
		{
			name:    "whitespace candidates are skipped without trimming the chosen name",
			contact: types.ContactInfo{FullName: " \t", PushName: "  Stored Push  ", BusinessName: "Business Name"},
			live:    " \n",
			want:    "  Stored Push  ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PreferredContactDisplayName(tt.contact, tt.live); got != tt.want {
				t.Fatalf("PreferredContactDisplayName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSenderDisplayNameResolver(t *testing.T) {
	ctx := context.Background()
	account := types.NewJID("628111111111", types.DefaultUserServer)
	other := types.NewJID("628222222222", types.DefaultUserServer)

	tests := []struct {
		name       string
		getter     contactInfoGetter
		sender     string
		isFromMe   bool
		livePush   string
		deviceName string
		want       string
	}{
		{
			name: "saved full name",
			getter: &senderDisplayNameContactGetter{contacts: map[types.JID]types.ContactInfo{
				other: {Found: true, FullName: "Saved Name", PushName: "Stored Push", BusinessName: "Business Name"},
			}},
			sender:   other.String(),
			livePush: "Live Push",
			want:     "Saved Name",
		},
		{
			name: "live push name",
			getter: &senderDisplayNameContactGetter{contacts: map[types.JID]types.ContactInfo{
				other: {Found: true, PushName: "Stored Push"},
			}},
			sender:   other.String(),
			livePush: "Live Push",
			want:     "Live Push",
		},
		{
			name: "stored push name",
			getter: &senderDisplayNameContactGetter{contacts: map[types.JID]types.ContactInfo{
				other: {Found: true, PushName: "Stored Push"},
			}},
			sender: other.String(),
			want:   "Stored Push",
		},
		{
			name: "business name",
			getter: &senderDisplayNameContactGetter{contacts: map[types.JID]types.ContactInfo{
				other: {Found: true, BusinessName: "Business Name"},
			}},
			sender: other.String(),
			want:   "Business Name",
		},
		{
			name:   "normal user identifier fallback",
			getter: &senderDisplayNameContactGetter{},
			sender: other.String(),
			want:   "628222222222",
		},
		{
			name:   "lid identifier fallback",
			getter: &senderDisplayNameContactGetter{},
			sender: "ABCD1234@lid",
			want:   "ABCD1234",
		},
		{
			name:   "malformed sender falls back to its full string",
			getter: &senderDisplayNameContactGetter{},
			sender: "not-a-jid",
			want:   "not-a-jid",
		},
		{
			name:       "supplied device display name wins for active account",
			getter:     &senderDisplayNameContactGetter{},
			sender:     "628111111111:4@s.whatsapp.net",
			isFromMe:   true,
			livePush:   "Live Push",
			deviceName: "Configured Device",
			want:       "Configured Device",
		},
		{
			name:     "live push name wins over active account identifier",
			getter:   &senderDisplayNameContactGetter{},
			sender:   account.String(),
			livePush: "Live Push",
			want:     "Live Push",
		},
		{
			name:   "active account falls back to account identifier",
			getter: &senderDisplayNameContactGetter{},
			sender: account.String(),
			want:   "628111111111",
		},
		{
			name:   "contact lookup error falls back to identifier",
			getter: &senderDisplayNameContactGetter{err: errors.New("contact lookup failed")},
			sender: other.String(),
			want:   "628222222222",
		},
		{
			name:   "nil contact lookup falls back to identifier",
			sender: other.String(),
			want:   "628222222222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := newSenderDisplayNameResolver(tt.getter, nil, account, tt.deviceName)
			if got := resolver.Resolve(ctx, tt.sender, tt.isFromMe, tt.livePush); got != tt.want {
				t.Fatalf("Resolve(%q, %t, %q) = %q, want %q", tt.sender, tt.isFromMe, tt.livePush, got, tt.want)
			}
		})
	}
}

func TestSenderDisplayNameCache(t *testing.T) {
	ctx := context.Background()
	account := types.NewJID("628111111111", types.DefaultUserServer)
	other := types.NewJID("628222222222", types.DefaultUserServer)
	getter := &senderDisplayNameContactGetter{contacts: map[types.JID]types.ContactInfo{
		account: {Found: true, PushName: "My Push"},
		other:   {Found: true, FullName: "Other Person"},
	}}
	resolver := newSenderDisplayNameResolver(getter, nil, account, "")

	cache := NewSenderDisplayNameCache(resolver)
	if got := cache.Resolve(ctx, other.String(), false, ""); got != "Other Person" {
		t.Fatalf("first other-person Resolve() = %q, want %q", got, "Other Person")
	}
	if got := cache.Resolve(ctx, other.String(), false, "Reaction Push"); got != "Other Person" {
		t.Fatalf("second other-person Resolve() = %q, want %q", got, "Other Person")
	}
	if getter.calls != 1 {
		t.Fatalf("other-person contact lookups = %d, want 1", getter.calls)
	}

	secondCache := NewSenderDisplayNameCache(resolver)
	if got := secondCache.Resolve(ctx, other.String(), false, ""); got != "Other Person" {
		t.Fatalf("second cache Resolve() = %q, want %q", got, "Other Person")
	}
	if getter.calls != 2 {
		t.Fatalf("contact lookups after second cache = %d, want 2", getter.calls)
	}

	selfCache := NewSenderDisplayNameCache(resolver)
	if got := selfCache.Resolve(ctx, "628111111111:4@s.whatsapp.net", true, ""); got != "My Push" {
		t.Fatalf("first self Resolve() = %q, want %q", got, "My Push")
	}
	if got := selfCache.Resolve(ctx, account.String(), true, ""); got != "My Push" {
		t.Fatalf("second self Resolve() = %q, want %q", got, "My Push")
	}
	if getter.calls != 3 {
		t.Fatalf("self contact lookups = %d, want 3", getter.calls)
	}
}
