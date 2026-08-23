package usecase

import (
	"context"
	"testing"
	"time"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// chatDisplayName keeps the existing table-driven fallback coverage focused on
// the production resolver after the local helper moved to infrastructure.
func chatDisplayName(jid, name string) string {
	ctx := context.Background()
	return whatsapp.NewChatDisplayNameResolver(ctx, nil).Resolve(ctx, jid, name)
}

// chatContactStoreStub serves the synced address book that the chat usecases
// read through the active device's whatsmeow client. Only the two read paths
// used by the resolver carry behaviour; the writes are unused here.
type chatContactStoreStub struct {
	contacts map[types.JID]types.ContactInfo
}

func (s *chatContactStoreStub) GetContact(_ context.Context, user types.JID) (types.ContactInfo, error) {
	return s.contacts[user.ToNonAD()], nil
}

func (s *chatContactStoreStub) GetAllContacts(context.Context) (map[types.JID]types.ContactInfo, error) {
	return s.contacts, nil
}

func (s *chatContactStoreStub) PutPushName(context.Context, types.JID, string) (bool, string, error) {
	return false, "", nil
}

func (s *chatContactStoreStub) PutBusinessName(context.Context, types.JID, string) (bool, string, error) {
	return false, "", nil
}

func (s *chatContactStoreStub) PutContactName(context.Context, types.JID, string, string) error {
	return nil
}

func (s *chatContactStoreStub) PutAllContactNames(context.Context, []store.ContactEntry) error {
	return nil
}

func (s *chatContactStoreStub) PutManyRedactedPhones(context.Context, []store.RedactedPhoneEntry) error {
	return nil
}

// chatDisplayNameContext mirrors how the REST/MCP layers hand the active device
// to the usecases, so these tests exercise the real ClientFromContext path.
func chatDisplayNameContext(accountJID types.JID, contacts map[types.JID]types.ContactInfo) context.Context {
	client := &whatsmeow.Client{Store: &store.Device{
		ID:       &accountJID,
		Contacts: &chatContactStoreStub{contacts: contacts},
	}}
	return whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(accountJID.String(), client, nil))
}

// TestListChatsResolvesSyncedContactNames pins the ListChats half of issue #805:
// a stored phone/JID placeholder must be replaced by the synced contact name,
// while an explicit chat label is still returned verbatim.
func TestListChatsResolvesSyncedContactNames(t *testing.T) {
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	alice := types.NewJID("628123456789", types.DefaultUserServer)
	bob := types.NewJID("628111111111", types.DefaultUserServer)
	now := time.Date(2026, time.May, 16, 8, 0, 0, 0, time.UTC)

	repo := &chatUsecaseRepoStub{chats: []*domainChatStorage.Chat{
		{DeviceID: accountJID.String(), JID: alice.String(), Name: alice.User, LastMessageTime: now, CreatedAt: now, UpdatedAt: now},
		{DeviceID: accountJID.String(), JID: bob.String(), Name: "Support Queue", LastMessageTime: now, CreatedAt: now, UpdatedAt: now},
	}}
	ctx := chatDisplayNameContext(accountJID, map[types.JID]types.ContactInfo{
		alice: {Found: true, FullName: "Saved Alice"},
		bob:   {Found: true, FullName: "Saved Bob"},
	})

	response, err := NewChatService(repo).ListChats(ctx, domainChat.ListChatsRequest{})
	if err != nil {
		t.Fatalf("list chats: %v", err)
	}
	if len(response.Data) != 2 {
		t.Fatalf("expected two chats, got %d", len(response.Data))
	}
	if response.Data[0].Name != "Saved Alice" {
		t.Fatalf("placeholder chat name = %q, want synced contact name", response.Data[0].Name)
	}
	if response.Data[1].Name != "Support Queue" {
		t.Fatalf("explicit chat name = %q, want it preserved", response.Data[1].Name)
	}
}

// TestGetChatMessagesResolvesSyncedContactNameForEmptyChat covers the
// not-yet-persisted chat response, where chat_info has no stored name at all.
func TestGetChatMessagesResolvesSyncedContactNameForEmptyChat(t *testing.T) {
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	alice := types.NewJID("628123456789", types.DefaultUserServer)

	ctx := chatDisplayNameContext(accountJID, map[types.JID]types.ContactInfo{
		alice: {Found: true, FullName: "Saved Alice"},
	})

	response, err := NewChatService(&chatUsecaseRepoStub{}).GetChatMessages(ctx, domainChat.GetChatMessagesRequest{
		ChatJID: alice.String(),
	})
	if err != nil {
		t.Fatalf("get chat messages: %v", err)
	}
	if len(response.Data) != 0 {
		t.Fatalf("expected empty message list, got %d", len(response.Data))
	}
	if response.ChatInfo.Name != "Saved Alice" {
		t.Fatalf("empty chat_info name = %q, want synced contact name", response.ChatInfo.Name)
	}
}

// TestGetChatMessagesResolvesSyncedContactNameForLoadedChat covers the stored
// chat response, where chat_info carries a phone-number placeholder.
func TestGetChatMessagesResolvesSyncedContactNameForLoadedChat(t *testing.T) {
	accountJID := types.NewJID("628999999999", types.DefaultUserServer)
	alice := types.NewJID("628123456789", types.DefaultUserServer)
	now := time.Date(2026, time.May, 16, 8, 0, 0, 0, time.UTC)

	repo := &chatUsecaseRepoStub{
		chat: &domainChatStorage.Chat{
			DeviceID:        accountJID.String(),
			JID:             alice.String(),
			Name:            alice.User,
			LastMessageTime: now,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		messages: []*domainChatStorage.Message{
			{
				ID:        "msg-1",
				ChatJID:   alice.String(),
				DeviceID:  accountJID.String(),
				Sender:    alice.String(),
				Content:   "hi",
				Timestamp: now,
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	ctx := chatDisplayNameContext(accountJID, map[types.JID]types.ContactInfo{
		alice: {Found: true, FullName: "Saved Alice"},
	})

	response, err := NewChatService(repo).GetChatMessages(ctx, domainChat.GetChatMessagesRequest{
		ChatJID: alice.String(),
	})
	if err != nil {
		t.Fatalf("get chat messages: %v", err)
	}
	if len(response.Data) != 1 {
		t.Fatalf("expected one message, got %d", len(response.Data))
	}
	if response.ChatInfo.Name != "Saved Alice" {
		t.Fatalf("loaded chat_info name = %q, want synced contact name", response.ChatInfo.Name)
	}
}
