package usecase

import (
	"context"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// storedMediaMessage builds a sent media message with its download details, as
// chat storage holds it for the calling device.
func storedMediaMessage(id, mediaType, filename string) domainChatStorage.Message {
	return domainChatStorage.Message{
		ID:            id,
		ChatJID:       "628123456789@s.whatsapp.net",
		DeviceID:      "device-a@s.whatsapp.net",
		Sender:        "device-a@s.whatsapp.net",
		Content:       "old caption",
		Timestamp:     time.Date(2026, time.August, 22, 11, 0, 0, 0, time.UTC),
		IsFromMe:      true,
		MediaType:     mediaType,
		Filename:      filename,
		URL:           "https://mmg.whatsapp.net/" + id,
		DirectPath:    "/v/t62.7118-24/" + id,
		MediaKey:      []byte("media-key-" + id),
		FileSHA256:    []byte("file-sha256-" + id),
		FileEncSHA256: []byte("file-enc-sha256-" + id),
		FileLength:    2048,
	}
}

// captionEditTestService stores media for the calling device, captures the
// edited content that UpdateMessage hands to WhatsApp, and reads the stored copy
// back.
func captionEditTestService(t *testing.T, media domainChatStorage.Message) (serviceMessage, func() *waE2E.Message, context.Context, func() *domainChatStorage.Message) {
	t.Helper()
	service, repo, ctx := newMessageActionTestService(t, nil)
	require.NoError(t, repo.StoreMessage(&media))

	var sent *waE2E.Message
	service.sendMessageFn = func(_ context.Context, _ *whatsmeow.Client, _ types.JID, message *waE2E.Message) (whatsmeow.SendResponse, error) {
		sent = message
		return whatsmeow.SendResponse{ID: "edit-event-1"}, nil
	}
	edited := func() *waE2E.Message {
		require.NotNil(t, sent)
		return sent.GetEditedMessage().GetMessage().GetProtocolMessage().GetEditedMessage()
	}
	stored := func() *domainChatStorage.Message {
		msg, err := repo.GetMessageByIDAndDevice(media.DeviceID, media.ID)
		require.NoError(t, err)
		require.NotNil(t, msg)
		return msg
	}
	return service, edited, ctx, stored
}

func TestUpdateMessageWithMediaTypeEditsCaption(t *testing.T) {
	tests := []struct {
		name    string
		media   domainChatStorage.Message
		caption func(*waE2E.Message) (string, bool)
	}{
		{
			name:  "image",
			media: storedMediaMessage("image-1", "image", "photo.jpg"),
			caption: func(m *waE2E.Message) (string, bool) {
				return m.GetImageMessage().GetCaption(), m.GetImageMessage() != nil
			},
		},
		{
			name:  "document",
			media: storedMediaMessage("document-1", "document", "invoice.pdf"),
			caption: func(m *waE2E.Message) (string, bool) {
				return m.GetDocumentMessage().GetCaption(), m.GetDocumentMessage() != nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, edited, ctx, stored := captionEditTestService(t, tt.media)

			_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
				MessageID: tt.media.ID,
				Phone:     tt.media.ChatJID,
				Message:   "new caption",
				MediaType: tt.media.MediaType,
			})
			require.NoError(t, err)

			caption, ok := tt.caption(edited())
			require.True(t, ok, "a %s caption edit must carry the %s message type", tt.name, tt.name)
			assert.Equal(t, "new caption", caption)
			assert.Empty(t, edited().GetConversation())

			got := stored()
			assert.Equal(t, "new caption", got.Content)
			assert.Equal(t, tt.media.MediaType, got.MediaType, "the edit must keep the media type")
			assert.Equal(t, tt.media.Filename, got.Filename, "the edit must keep the filename")
			assert.Equal(t, tt.media.URL, got.URL, "the edit must keep the download URL")
			assert.Equal(t, tt.media.DirectPath, got.DirectPath, "the edit must keep the direct path")
			assert.Equal(t, tt.media.MediaKey, got.MediaKey, "the edit must keep the media key")
			assert.Equal(t, tt.media.FileSHA256, got.FileSHA256, "the edit must keep the file hash")
			assert.Equal(t, tt.media.FileEncSHA256, got.FileEncSHA256, "the edit must keep the encrypted file hash")
			assert.Equal(t, tt.media.FileLength, got.FileLength, "the edit must keep the file length")
		})
	}
}

func TestUpdateMessageWithMediaTypeLeavesUncachedOriginalOutOfStorage(t *testing.T) {
	tests := []struct {
		mediaType string
		caption   func(*waE2E.Message) (string, bool)
	}{
		{"image", func(m *waE2E.Message) (string, bool) {
			return m.GetImageMessage().GetCaption(), m.GetImageMessage() != nil
		}},
		{"document", func(m *waE2E.Message) (string, bool) {
			return m.GetDocumentMessage().GetCaption(), m.GetDocumentMessage() != nil
		}},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			service, repo, ctx := newMessageActionTestService(t, nil)
			messageID := "uncached-" + tt.mediaType
			var sent *waE2E.Message
			service.sendMessageFn = func(_ context.Context, _ *whatsmeow.Client, _ types.JID, message *waE2E.Message) (whatsmeow.SendResponse, error) {
				sent = message
				return whatsmeow.SendResponse{ID: "edit-event-1"}, nil
			}

			_, err := service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
				MessageID: messageID,
				Phone:     "628123456789@s.whatsapp.net",
				Message:   "new caption",
				MediaType: tt.mediaType,
			})
			require.NoError(t, err)

			require.NotNil(t, sent)
			caption, ok := tt.caption(sent.GetEditedMessage().GetMessage().GetProtocolMessage().GetEditedMessage())
			require.True(t, ok, "the edit must still be sent as a %s caption edit", tt.mediaType)
			assert.Equal(t, "new caption", caption)

			got, err := repo.GetMessageByIDAndDevice("device-a@s.whatsapp.net", messageID)
			require.NoError(t, err)
			assert.Nil(t, got, "a message missing from storage must not gain a row whose type was never checked")
		})
	}
}

func TestUpdateMessageRejectsMediaTypeOfAnotherStoredType(t *testing.T) {
	tests := []struct {
		name      string
		messageID string
		mediaType string
		wantErr   string
	}{
		{"image type on a document", "document-1", "image", "media_type: message document-1 is document, not image."},
		{"document type on an image", "image-1", "document", "media_type: message image-1 is image, not document."},
		{"image type on a text message", "message-1", "image", "media_type: message message-1 is text, not image."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// newMessageActionTestService already stores message-1, a text message.
			service, repo, ctx := newMessageActionTestService(t, nil)
			image := storedMediaMessage("image-1", "image", "photo.jpg")
			document := storedMediaMessage("document-1", "document", "invoice.pdf")
			require.NoError(t, repo.StoreMessage(&image))
			require.NoError(t, repo.StoreMessage(&document))
			service.sendMessageFn = func(_ context.Context, _ *whatsmeow.Client, _ types.JID, _ *waE2E.Message) (whatsmeow.SendResponse, error) {
				t.Fatal("a mismatched caption edit must not reach WhatsApp")
				return whatsmeow.SendResponse{}, nil
			}
			before, err := repo.GetMessageByIDAndDevice("device-a@s.whatsapp.net", tt.messageID)
			require.NoError(t, err)
			require.NotNil(t, before)

			_, err = service.UpdateMessage(ctx, domainMessage.UpdateMessageRequest{
				MessageID: tt.messageID,
				Phone:     "628123456789@s.whatsapp.net",
				Message:   "new caption",
				MediaType: tt.mediaType,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)

			after, err := repo.GetMessageByIDAndDevice("device-a@s.whatsapp.net", tt.messageID)
			require.NoError(t, err)
			require.NotNil(t, after)
			assert.Equal(t, before.Content, after.Content, "a refused edit must leave the stored copy unchanged")
		})
	}
}
