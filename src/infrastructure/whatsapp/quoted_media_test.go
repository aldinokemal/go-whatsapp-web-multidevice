package whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// History copies ride in the message batch, so they are written by its
// statements, in its transaction.
func TestProcessHistorySyncStoresQuotedMediaWithTheBatch(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	t.Cleanup(func() { log = originalLog })

	chatJID := "550000000002@s.whatsapp.net"
	thumb := []byte{0xFF, 0xD8}
	timestamp := uint64(time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC).Unix())
	history := func(id string, message *waE2E.Message) *waHistorySync.HistorySyncMsg {
		return &waHistorySync.HistorySyncMsg{Message: &waWeb.WebMessageInfo{
			Key:     &waCommon.MessageKey{RemoteJID: proto.String(chatJID), FromMe: proto.Bool(false), ID: proto.String(id)},
			Message: message, MessageTimestamp: &timestamp,
		}}
	}
	wrap := func(m *waE2E.Message) *waE2E.FutureProofMessage { return &waE2E.FutureProofMessage{Message: m} }
	note := &waE2E.VideoMessage{Caption: proto.String("ptv"), Seconds: proto.Uint32(7), JPEGThumbnail: thumb}
	repo := &historyMessageBatchRepoSpy{}
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance("device-a@s.whatsapp.net", nil, nil))
	data := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{
		ID: proto.String(chatJID),
		Messages: []*waHistorySync.HistorySyncMsg{
			history("video", &waE2E.Message{EphemeralMessage: wrap(&waE2E.Message{VideoMessage: &waE2E.VideoMessage{
				JPEGThumbnail: thumb, ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("PARENT")},
			}})}),
			history("view-once", &waE2E.Message{ViewOnceMessageV2: wrap(&waE2E.Message{ImageMessage: &waE2E.ImageMessage{
				Mimetype: proto.String("image/jpeg"), JPEGThumbnail: thumb, MediaKey: []byte{9},
			}})}),
			history("text", &waE2E.Message{Conversation: proto.String("hello")}),
			history("video note", &waE2E.Message{PtvMessage: note}),
		},
	}}}

	require.NoError(t, processConversationMessages(ctx, data, repo, nil))

	want := map[string]*waE2E.Message{
		"video":      {VideoMessage: &waE2E.VideoMessage{JPEGThumbnail: thumb}},
		"view-once":  {ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), ViewOnce: proto.Bool(true)}},
		"text":       nil,
		"video note": {PtvMessage: &waE2E.VideoMessage{Caption: proto.String("ptv"), Seconds: proto.Uint32(7), JPEGThumbnail: thumb}},
	}
	require.Equal(t, 1, repo.storeMessagesBatchCalls)
	require.Len(t, repo.lastBatch, len(want))
	for _, message := range repo.lastBatch {
		if want[message.ID] == nil {
			require.Nil(t, message.QuotedMedia, message.ID)
			continue
		}
		decoded := &waE2E.Message{}
		require.NoError(t, proto.Unmarshal(message.QuotedMedia, decoded), message.ID)
		require.True(t, proto.Equal(want[message.ID], decoded), "%s copy %v, want %v", message.ID, decoded, want[message.ID])
		if message.ID == "video note" {
			require.Equal(t, "ptv", message.Content)
		}
	}
}

type quotedMediaLookupSpy struct {
	domainChatStorage.IChatStorageRepository
	ctx  context.Context
	args []string
	err  error
}

func (s *quotedMediaLookupSpy) GetQuotedMedia(ctx context.Context, deviceID, chatJID, id string) (*domainChatStorage.QuotedMedia, error) {
	s.ctx = ctx
	s.args = []string{deviceID, chatJID, id}
	return nil, s.err
}

func TestDeviceChatStorageGetQuotedMediaUsesItsDevice(t *testing.T) {
	type ctxKey struct{}
	for device, want := range map[string]string{"": "device-a@s.whatsapp.net", "device-b@s.whatsapp.net": "device-b@s.whatsapp.net"} {
		spy := &quotedMediaLookupSpy{err: errors.New("database is locked")}
		ctx := context.WithValue(context.Background(), ctxKey{}, device)

		_, err := newDeviceChatStorage("device-a@s.whatsapp.net", spy).GetQuotedMedia(ctx, device, "120363000000000001@g.us", "m1")

		require.ErrorIs(t, err, spy.err)
		require.Equal(t, []string{want, "120363000000000001@g.us", "m1"}, spy.args, "device %q", device)
		require.Equal(t, ctx, spy.ctx)
	}
}
