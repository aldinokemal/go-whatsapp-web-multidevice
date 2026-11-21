package whatsapp

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
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

// historyChatNameRepoSpy models the repository's group fallback, including a
// meaningful device-scoped name saved before the history chunk arrived.
type historyChatNameRepoSpy struct {
	*historyMessageBatchRepoSpy
	storedName string
}

func (r *historyChatNameRepoSpy) GetChatNameWithPushNameByDevice(_ string, jid types.JID, _ string, _, _ string) string {
	if r.storedName != "" {
		return r.storedName
	}
	if jid.Server == types.GroupServer {
		return "Group " + jid.User
	}
	if jid.Server == types.NewsletterServer {
		return "Newsletter " + jid.User
	}
	return jid.User
}

func TestHistorySyncPersistsGroupSubjectInChatName(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })
	client, ctx := groupNameTestDevice(t, "15550000991")
	originalFetch := fetchGroupInfoFn
	fetches := 0
	fetchGroupInfoFn = func(_ context.Context, gotClient *whatsmeow.Client, jid types.JID) (*types.GroupInfo, error) {
		if gotClient != client {
			t.Fatal("group info fetched from the wrong device")
		}
		fetches++
		return &types.GroupInfo{JID: jid, GroupName: types.GroupName{Name: "Fetched subject"}}, nil
	}
	t.Cleanup(func() { fetchGroupInfoFn = originalFetch })

	for _, tc := range []struct {
		name, id, displayName, storedName, want string
		fetches                                 int
		syncType                                waHistorySync.HistorySync_HistorySyncType
	}{
		{"display name replaces placeholder", "120363000000009901", "History subject", "", "History subject", 0, waHistorySync.HistorySync_RECENT},
		{"display name replaces saved placeholder", "120363000000009905", "History subject", "Group 120363000000009905", "History subject", 0, waHistorySync.HistorySync_ON_DEMAND},
		{"stored name survives on demand", "120363000000009902", "History subject", "Saved subject", "Saved subject", 0, waHistorySync.HistorySync_ON_DEMAND},
		{"missing display name fetches subject", "120363000000009903", "", "", "Fetched subject", 1, waHistorySync.HistorySync_RECENT},
		{"stored name needs no fetch", "120363000000009904", "", "Saved subject", "Saved subject", 0, waHistorySync.HistorySync_RECENT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jid := types.NewJID(tc.id, types.GroupServer)
			repo := &historyChatNameRepoSpy{historyMessageBatchRepoSpy: &historyMessageBatchRepoSpy{}, storedName: tc.storedName}
			if tc.syncType == waHistorySync.HistorySync_ON_DEMAND {
				repo.existingChat = &domainChatStorage.Chat{DeviceID: client.Store.ID.ToNonAD().String(), JID: jid.String(), Name: tc.storedName, LastMessageTime: time.Now().Add(time.Hour), Archived: true}
			}
			stamp := uint64(time.Now().Unix())
			data := &waHistorySync.HistorySync{SyncType: &tc.syncType, Conversations: []*waHistorySync.Conversation{{
				ID: proto.String(jid.String()), DisplayName: proto.String(tc.displayName),
				Messages: []*waHistorySync.HistorySyncMsg{{Message: &waWeb.WebMessageInfo{
					Key:     &waCommon.MessageKey{ID: proto.String("message-1"), Participant: proto.String("15550000992@s.whatsapp.net")},
					Message: &waE2E.Message{Conversation: proto.String("hello")}, MessageTimestamp: &stamp,
				}}},
			}}}
			before := fetches
			if err := processConversationMessages(ctx, data, repo, client); err != nil {
				t.Fatal(err)
			}
			if repo.lastStoredChat == nil || repo.lastStoredChat.Name != tc.want {
				t.Fatalf("persisted chat = %+v, want name %q", repo.lastStoredChat, tc.want)
			}
			if fetches-before != tc.fetches {
				t.Fatalf("group info fetches = %d, want %d", fetches-before, tc.fetches)
			}
			if tc.syncType == waHistorySync.HistorySync_ON_DEMAND && (!repo.lastStoredChat.Archived || !repo.lastStoredChat.LastMessageTime.Equal(repo.existingChat.LastMessageTime)) {
				t.Fatalf("on-demand chat metadata changed: %+v", repo.lastStoredChat)
			}
		})
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
