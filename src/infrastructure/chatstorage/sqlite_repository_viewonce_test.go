package chatstorage

import (
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A view-once "unavailable" placeholder carries no delivered media, so it must
// never surface in media-only results (GetMessages MediaOnly) nor mark its chat
// as having media (GetChats HasMedia) — even though it DOES carry a media_type.
// The row is stamped with the view_once_unavailable discriminator so history
// importers can recognise it; the two media filters exclude that value
// explicitly, exactly like the synthetic "call" rows. Real media in the same
// chat must still pass both filters.
func TestViewOncePlaceholderExcludedFromMediaFilters(t *testing.T) {
	deviceID := "628987654321@s.whatsapp.net"
	placeholderChat := "628123456789@s.whatsapp.net"
	mediaChat := "628111222333@s.whatsapp.net"

	newRepo := func(t *testing.T) *SQLiteRepository {
		repo := newTestSQLiteRepository(t)
		require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{
			DeviceID: deviceID, JID: placeholderChat, Name: "John Doe", LastMessageTime: time.Now(),
		}))
		require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
			ID:        "3EB0C127D7BACC83D6B9",
			ChatJID:   placeholderChat,
			DeviceID:  deviceID,
			Sender:    placeholderChat,
			Content:   "(View once message \u2014 for privacy reasons it can only be opened on the phone)",
			MediaType: domainChatStorage.MediaTypeViewOnceUnavailable,
			Timestamp: time.Now(),
		}))
		return repo
	}

	t.Run("the discriminator is persisted", func(t *testing.T) {
		repo := newRepo(t)
		stored, err := repo.GetMessageByIDAndDevice(deviceID, "3EB0C127D7BACC83D6B9")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, domainChatStorage.MediaTypeViewOnceUnavailable, stored.MediaType,
			"history importers read this to flag the Chatwoot link as a placeholder")
	})

	t.Run("excluded from GetMessages MediaOnly", func(t *testing.T) {
		repo := newRepo(t)
		mediaOnly, err := repo.GetMessages(&domainChatStorage.MessageFilter{
			DeviceID: deviceID, ChatJID: placeholderChat, MediaOnly: true,
		})
		require.NoError(t, err)
		assert.Empty(t, mediaOnly, "placeholder leaked into media-only results")
	})

	t.Run("excluded from GetChats HasMedia", func(t *testing.T) {
		repo := newRepo(t)
		withMedia, err := repo.GetChats(&domainChatStorage.ChatFilter{
			DeviceID: deviceID, HasMedia: true,
		})
		require.NoError(t, err)
		assert.Empty(t, withMedia, "placeholder marked its chat as having media")
	})

	t.Run("real media still passes both filters", func(t *testing.T) {
		repo := newRepo(t)
		require.NoError(t, repo.StoreChat(&domainChatStorage.Chat{
			DeviceID: deviceID, JID: mediaChat, Name: "Jane Doe", LastMessageTime: time.Now(),
		}))
		require.NoError(t, repo.StoreMessage(&domainChatStorage.Message{
			ID:        "3EB0C127D7BACC83D6BA",
			ChatJID:   mediaChat,
			DeviceID:  deviceID,
			Sender:    mediaChat,
			MediaType: "image",
			Timestamp: time.Now(),
		}))

		mediaOnly, err := repo.GetMessages(&domainChatStorage.MessageFilter{
			DeviceID: deviceID, ChatJID: mediaChat, MediaOnly: true,
		})
		require.NoError(t, err)
		assert.Len(t, mediaOnly, 1)

		withMedia, err := repo.GetChats(&domainChatStorage.ChatFilter{
			DeviceID: deviceID, HasMedia: true,
		})
		require.NoError(t, err)
		require.Len(t, withMedia, 1, "only the chat with real media qualifies")
		assert.Equal(t, mediaChat, withMedia[0].JID)
	})
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
		{"an ordinary upsert never writes the flag back", func(t *testing.T) {
			// The read-receipt and rebind paths load a link, change one field and
			// upsert the whole struct. Their copy of the flag is stale by then, so
			// an upsert that carried it would release a claim taken meanwhile.
			require.NoError(t, repo.UpsertChatwootMessageLink(link(devA, false)))
			a, err := repo.GetChatwootMessageLinkByWhatsAppID(devA, waID)
			require.NoError(t, err)
			assert.True(t, a.IsViewOncePlaceholder, "a stale upsert must not clear the claim state")
			require.NoError(t, repo.UpsertChatwootMessageLink(link(devB, true)))
			b, err := repo.GetChatwootMessageLinkByWhatsAppID(devB, waID)
			require.NoError(t, err)
			assert.False(t, b.IsViewOncePlaceholder, "nor set it")
		}},
		{"the explicit setter is the only way to change it", func(t *testing.T) {
			require.NoError(t, repo.SetChatwootLinkViewOncePlaceholder(devA, waID, false))
			a, err := repo.GetChatwootMessageLinkByWhatsAppID(devA, waID)
			require.NoError(t, err)
			assert.False(t, a.IsViewOncePlaceholder)
			b, err := repo.GetChatwootMessageLinkByWhatsAppID(devB, waID)
			require.NoError(t, err)
			assert.False(t, b.IsViewOncePlaceholder, "another device's row is untouched")
		}},
		{"delete removes only its own device row", func(t *testing.T) {
			require.NoError(t, repo.DeleteChatwootMessageLink(devA, waID))
			a, err := repo.GetChatwootMessageLinkByWhatsAppID(devA, waID)
			require.NoError(t, err)
			assert.Nil(t, a, "the reservation rollback must drop the row entirely")
			b, err := repo.GetChatwootMessageLinkByWhatsAppID(devB, waID)
			require.NoError(t, err)
			assert.NotNil(t, b, "same wa_message_id on another device must survive")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
