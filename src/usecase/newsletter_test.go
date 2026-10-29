package usecase

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

func TestFindNewsletterMessage(t *testing.T) {
	target := &types.NewsletterMessage{MessageServerID: 8872, MessageID: "target"}

	tests := []struct {
		name     string
		messages []*types.NewsletterMessage
		serverID int
		want     *types.NewsletterMessage
		wantErr  string
	}{
		{
			name:     "finds exact server id",
			messages: []*types.NewsletterMessage{{MessageServerID: 8871}, target},
			serverID: 8872,
			want:     target,
		},
		{
			name:     "rejects empty result",
			serverID: 8872,
			wantErr:  "newsletter message with server ID 8872 not found",
		},
		{
			name:     "rejects mismatched result",
			messages: []*types.NewsletterMessage{{MessageServerID: 8871}},
			serverID: 8872,
			wantErr:  "newsletter message with server ID 8872 not found",
		},
		{
			name:     "ignores nil entries",
			messages: []*types.NewsletterMessage{nil},
			serverID: 8872,
			wantErr:  "newsletter message with server ID 8872 not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findNewsletterMessage(tt.messages, tt.serverID)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Nil(t, got)
				return
			}

			require.NoError(t, err)
			assert.Same(t, tt.want, got)
		})
	}
}

func TestNewsletterDownloadableMedia(t *testing.T) {
	tests := []struct {
		name      string
		message   *waE2E.Message
		mediaType string
		wantErr   string
	}{
		{name: "image", message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, mediaType: "image"},
		{name: "video", message: &waE2E.Message{VideoMessage: &waE2E.VideoMessage{}}, mediaType: "video"},
		{name: "video note", message: &waE2E.Message{PtvMessage: &waE2E.VideoMessage{}}, mediaType: "video_note"},
		{name: "audio", message: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{}}, mediaType: "audio"},
		{name: "document", message: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{}}, mediaType: "document"},
		{name: "sticker", message: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}, mediaType: "sticker"},
		{name: "nil message", wantErr: "newsletter message does not contain downloadable media"},
		{name: "text only", message: &waE2E.Message{Conversation: stringPointer("hello")}, wantErr: "newsletter message does not contain downloadable media"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			media, mediaType, err := newsletterDownloadableMedia(tt.message)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				assert.Nil(t, media)
				assert.Empty(t, mediaType)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, media)
			assert.Equal(t, tt.mediaType, mediaType)
		})
	}
}

func stringPointer(value string) *string {
	return &value
}
