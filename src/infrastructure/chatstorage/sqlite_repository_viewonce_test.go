package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// A view-once "unavailable" placeholder carries no delivered media, so it must
// never surface in media-only results (GetMessages MediaOnly) nor mark its
// chat as having media (GetChats HasMedia). The placeholder row uses an empty
// media_type on purpose — this test pins that contract.
func TestViewOncePlaceholderExcludedFromMediaFilters(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	deviceID := "628987654321@s.whatsapp.net"
	chatJID := "628123456789@s.whatsapp.net"

	if err := repo.StoreChat(&domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            "John Doe",
		LastMessageTime: time.Now(),
	}); err != nil {
		t.Fatalf("store chat: %v", err)
	}
	if err := repo.StoreMessage(&domainChatStorage.Message{
		ID:        "3EB0C127D7BACC83D6B9",
		ChatJID:   chatJID,
		DeviceID:  deviceID,
		Sender:    chatJID,
		Content:   "(View once message — for privacy reasons it can only be opened on the phone)",
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("store placeholder: %v", err)
	}

	mediaOnly, err := repo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID: deviceID, ChatJID: chatJID, MediaOnly: true,
	})
	if err != nil {
		t.Fatalf("get messages media-only: %v", err)
	}
	if len(mediaOnly) != 0 {
		t.Fatalf("placeholder leaked into media-only results: %+v", mediaOnly)
	}

	withMedia, err := repo.GetChats(&domainChatStorage.ChatFilter{
		DeviceID: deviceID, HasMedia: true,
	})
	if err != nil {
		t.Fatalf("get chats has-media: %v", err)
	}
	if len(withMedia) != 0 {
		t.Fatalf("placeholder marked the chat as having media: %+v", withMedia)
	}

	// Sanity: the row itself IS persisted and readable.
	stored, err := repo.GetMessageByIDAndDevice(deviceID, "3EB0C127D7BACC83D6B9")
	if err != nil || stored == nil {
		t.Fatalf("placeholder row missing: %v %v", stored, err)
	}
	if stored.MediaType != "" {
		t.Fatalf("placeholder must not carry a media_type, got %q", stored.MediaType)
	}
}
