package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// Lifecycle of ChatwootMessageLink.IsViewOncePlaceholder: persists on upsert,
// survives the read path, flips via the atomic claim (single winner), is
// restorable via release, and stays isolated per device_id.
func TestChatwootLinkViewOncePlaceholderLifecycle(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	waID := "3EB0C127D7BACC83D6C7"
	link := func(deviceID string, placeholder bool) *domainChatStorage.ChatwootMessageLink {
		return &domainChatStorage.ChatwootMessageLink{
			DeviceID: deviceID, WhatsAppMessageID: waID, WhatsAppChatJID: "628123@s.whatsapp.net",
			ChatwootMessageID: 42, ChatwootConversationID: 7, Direction: "incoming",
			IsViewOncePlaceholder: placeholder,
		}
	}
	devA, devB := "devA@s.whatsapp.net", "devB@s.whatsapp.net"
	require.NoError(t, repo.UpsertChatwootMessageLink(link(devA, true)))
	require.NoError(t, repo.UpsertChatwootMessageLink(link(devB, false)))

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"placeholder flag persists per device", func(t *testing.T) {
			a, err := repo.GetChatwootMessageLinkByWhatsAppID(devA, waID)
			require.NoError(t, err)
			assert.True(t, a.IsViewOncePlaceholder)
			b, err := repo.GetChatwootMessageLinkByWhatsAppID(devB, waID)
			require.NoError(t, err)
			assert.False(t, b.IsViewOncePlaceholder, "same wa_message_id on another device must stay isolated")
		}},
		{"claim wins exactly once and only touches its device", func(t *testing.T) {
			won, err := repo.ClaimViewOncePlaceholderUpgrade(devA, waID)
			require.NoError(t, err)
			assert.True(t, won)
			again, err := repo.ClaimViewOncePlaceholderUpgrade(devA, waID)
			require.NoError(t, err)
			assert.False(t, again, "second concurrent claim must lose")
			b, err := repo.GetChatwootMessageLinkByWhatsAppID(devB, waID)
			require.NoError(t, err)
			assert.False(t, b.IsViewOncePlaceholder)
		}},
		{"release restores the claim", func(t *testing.T) {
			require.NoError(t, repo.ReleaseViewOncePlaceholderUpgrade(devA, waID))
			a, err := repo.GetChatwootMessageLinkByWhatsAppID(devA, waID)
			require.NoError(t, err)
			assert.True(t, a.IsViewOncePlaceholder)
		}},
		{"upsert to false clears the flag", func(t *testing.T) {
			require.NoError(t, repo.UpsertChatwootMessageLink(link(devA, false)))
			a, err := repo.GetChatwootMessageLinkByWhatsAppID(devA, waID)
			require.NoError(t, err)
			assert.False(t, a.IsViewOncePlaceholder)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
