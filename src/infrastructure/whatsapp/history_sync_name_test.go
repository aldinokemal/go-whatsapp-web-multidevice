package whatsapp

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

type nameContactsSpy struct {
	contacts map[types.JID]types.ContactInfo
}

func (s nameContactsSpy) GetContact(_ context.Context, jid types.JID) (types.ContactInfo, error) {
	return s.contacts[jid], nil
}

func (s nameContactsSpy) GetAllContacts(_ context.Context) (map[types.JID]types.ContactInfo, error) {
	return s.contacts, nil
}

func TestConversationChatNamePrefersContactOverDisplayName(t *testing.T) {
	jid := types.NewJID("5491137991166", types.DefaultUserServer)
	spy := nameContactsSpy{contacts: map[types.JID]types.ContactInfo{
		jid: {Found: true, FullName: "Tuty Rivelli"},
	}}
	resolver := newChatDisplayNameResolver(spy, nil)

	// The owner's own push name arriving as a conversation DisplayName is the
	// case that leaves unrelated chats all named after the account owner.
	if got := conversationChatName(context.Background(), resolver, jid.String(), jid, "Adrian"); got != "Tuty Rivelli" {
		t.Fatalf("owner push name won over the contact: got %q", got)
	}
	// An empty DisplayName is what leaves a bare phone number as the chat name.
	if got := conversationChatName(context.Background(), resolver, jid.String(), jid, ""); got != "Tuty Rivelli" {
		t.Fatalf("empty display name did not fall back to the contact: got %q", got)
	}
}

func TestConversationChatNameKeepsDisplayNameWhenContactUnknown(t *testing.T) {
	jid := types.NewJID("5491100000000", types.DefaultUserServer)
	resolver := newChatDisplayNameResolver(nameContactsSpy{contacts: map[types.JID]types.ContactInfo{}}, nil)

	if got := conversationChatName(context.Background(), resolver, jid.String(), jid, "Some Label"); got != "Some Label" {
		t.Fatalf("discarded the display name with no contact to replace it: got %q", got)
	}
}

func TestConversationChatNameWithoutResolver(t *testing.T) {
	jid := types.NewJID("5491100000000", types.DefaultUserServer)
	if got := conversationChatName(context.Background(), nil, jid.String(), jid, "Some Label"); got != "Some Label" {
		t.Fatalf("nil resolver should be a no-op: got %q", got)
	}
}

// Groups and newsletters carry their real subject in DisplayName. Asking the resolver
// with no stored name answers "Group <id>" / "Newsletter <id>", so those servers must
// never reach it.
func TestConversationChatNameKeepsDisplayNameForGroupAndNewsletter(t *testing.T) {
	resolver := newChatDisplayNameResolver(nameContactsSpy{contacts: map[types.JID]types.ContactInfo{}}, nil)

	for _, tc := range []struct {
		name        string
		jid         types.JID
		displayName string
	}{
		{"group", types.NewJID("120363012345678901", types.GroupServer), "Family Group"},
		{"newsletter", types.NewJID("120363111111111111", types.NewsletterServer), "Tech Channel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := conversationChatName(context.Background(), resolver, tc.jid.String(), tc.jid, tc.displayName)
			if got != tc.displayName {
				t.Fatalf("expected %q to be kept for %s, got %q", tc.displayName, tc.jid, got)
			}
		})
	}
}
