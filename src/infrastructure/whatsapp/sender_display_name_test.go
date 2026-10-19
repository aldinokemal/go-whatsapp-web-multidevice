package whatsapp

import (
	"context"
	"errors"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

type senderDisplayNameContactGetter struct {
	store.NoopStore
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
			name: "supplied device display name wins for active account",
			getter: &senderDisplayNameContactGetter{contacts: map[types.JID]types.ContactInfo{
				account: {Found: true, FullName: "Saved Self", PushName: "Stored Self", BusinessName: "Self Business"},
			}},
			sender:     "628111111111:4@s.whatsapp.net",
			isFromMe:   true,
			livePush:   "Live Push",
			deviceName: "Configured Device",
			want:       "Configured Device",
		},
		{
			name: "active account ignores contact and live push names before identifier fallback",
			getter: &senderDisplayNameContactGetter{contacts: map[types.JID]types.ContactInfo{
				account: {Found: true, FullName: "Saved Self", PushName: "Stored Self", BusinessName: "Self Business"},
			}},
			sender:   account.String(),
			livePush: "Live Push",
			want:     "628111111111",
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

func TestNewSenderDisplayNameResolverDerivesClientPushNameForActiveAccount(t *testing.T) {
	ctx := context.Background()
	account := types.NewJID("628111111111", types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &store.Device{ID: &account, PushName: "Client Push"}}

	resolver := NewSenderDisplayNameResolver(client, "")
	if got := resolver.Resolve(ctx, account.String(), false, "Live Push"); got != "Client Push" {
		t.Fatalf("Resolve() = %q, want %q", got, "Client Push")
	}

	resolver = NewSenderDisplayNameResolver(client, "Configured Device")
	if got := resolver.Resolve(ctx, account.String(), false, "Live Push"); got != "Configured Device" {
		t.Fatalf("Resolve() = %q, want %q", got, "Configured Device")
	}
}

func TestSenderDisplayNameResolverFallsBackToFullAccountJID(t *testing.T) {
	resolver := newSenderDisplayNameResolver(nil, nil, types.NewJID("", "account.example"), "")

	if got := resolver.Resolve(context.Background(), "628222222222@s.whatsapp.net", true, "Live Push"); got != "account.example" {
		t.Fatalf("Resolve() = %q, want %q", got, "account.example")
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
	if got := selfCache.Resolve(ctx, "628111111111:4@s.whatsapp.net", true, "Live Push"); got != "628111111111" {
		t.Fatalf("first self Resolve() = %q, want %q", got, "628111111111")
	}
	if got := selfCache.Resolve(ctx, account.String(), true, ""); got != "628111111111" {
		t.Fatalf("second self Resolve() = %q, want %q", got, "628111111111")
	}
	if getter.calls != 2 {
		t.Fatalf("self contact lookups = %d, want 2", getter.calls)
	}
}

func TestAddSenderDisplayNameRequiresSingularNonEmptyFrom(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
	}{
		{
			name:    "missing from",
			payload: map[string]any{},
		},
		{
			name:    "empty from",
			payload: map[string]any{"from": ""},
		},
		{
			name:    "non string from",
			payload: map[string]any{"from": []string{"628123456789@s.whatsapp.net"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addSenderDisplayName(context.Background(), nil, tt.payload, false, "Live Push")

			if _, ok := tt.payload["sender_display_name"]; ok {
				t.Fatalf("sender_display_name unexpectedly added to payload %#v", tt.payload)
			}
		})
	}
}
