package chatstorage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// A newer edit that comes back in a placeholder-resend response, after an edit
// received live, replaces the text or caption and is recorded as its own edit
// event; the photo and the event passed in stay as they were, and a recovered
// replay of the original keeps the edited content. A secret edit the handler
// already materialized is stored as that edit.
func TestRecoveredEditAfterAnEditReplacesTheContent(t *testing.T) {
	chat := types.NewJID("628123456789", types.DefaultUserServer)
	sent := time.Date(2026, time.October, 18, 10, 0, 0, 0, time.UTC)
	text := func(s string) *waE2E.Message { return &waE2E.Message{Conversation: proto.String(s)} }
	caption := func(s string) *waE2E.Message {
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String(s)}}
	}
	// edit edits ORIGINAL to body; wrapped puts it in the FutureProofMessage
	// that whatsmeow's BuildEdit sends.
	edit := func(body *waE2E.Message, wrapped bool) *waE2E.Message {
		msg := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key:           &waCommon.MessageKey{RemoteJID: proto.String(orderChatJID), ID: proto.String("ORIGINAL")},
			EditedMessage: body,
		}}
		if wrapped {
			return &waE2E.Message{EditedMessage: &waE2E.FutureProofMessage{Message: msg}}
		}
		return msg
	}
	live := func(id string, msg *waE2E.Message, at time.Time) *events.Message {
		return (&events.Message{
			Info:       types.MessageInfo{ID: id, Timestamp: at, MessageSource: types.MessageSource{Chat: chat, Sender: chat}},
			RawMessage: msg,
		}).UnwrapRaw()
	}
	// recovered is msg as whatsmeow dispatches it from a placeholder-resend
	// response, parsed by the pinned ParseWebMessage.
	recovered := func(id string, msg *waE2E.Message, at time.Time, requestID string) *events.Message {
		evt, err := (&whatsmeow.Client{}).ParseWebMessage(types.EmptyJID, &waWeb.WebMessageInfo{
			Key:              &waCommon.MessageKey{RemoteJID: proto.String(orderChatJID), ID: proto.String(id)},
			MessageTimestamp: proto.Uint64(uint64(at.Unix())),
			Message:          msg,
		})
		require.NoError(t, err)
		evt.UnavailableRequestID = requestID
		return evt
	}

	for _, tc := range []struct {
		name      string
		original  *waE2E.Message
		edited    func(string) *waE2E.Message
		wrapped   bool
		secret    bool
		requestID string
	}{
		{name: "text", original: text("original"), edited: text, requestID: "RESEND-1"},
		{name: "caption_wrapped", original: qmPhoto("original", nil), edited: caption, wrapped: true},
		{name: "materialized_secret_edit", original: text("original"), edited: text, secret: true, requestID: "RESEND-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			ctx := qmDeviceContext(orderDevice)
			require.NoError(t, repo.CreateMessage(ctx, live("ORIGINAL", tc.original, sent)))
			require.NoError(t, repo.CreateMessage(ctx, live("EDIT-1", edit(tc.edited("first edit"), tc.wrapped), sent.Add(time.Minute))))

			second := edit(tc.edited("second edit"), tc.wrapped)
			if tc.secret {
				second = &waE2E.Message{SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{
					SecretEncType:    waE2E.SecretEncryptedMessage_MESSAGE_EDIT.Enum(),
					TargetMessageKey: &waCommon.MessageKey{RemoteJID: proto.String(orderChatJID), ID: proto.String("ORIGINAL")},
				}}
			}
			evt := recovered("EDIT-2", second, sent.Add(2*time.Minute), tc.requestID)
			if tc.secret { // as materializeSecretEditMessage leaves it
				evt.Message = edit(tc.edited("second edit"), false)
			}
			beforeInfo, beforeMessage := evt.Info, proto.Clone(evt.Message)
			require.NoError(t, repo.CreateMessage(ctx, evt))
			require.Equal(t, beforeInfo, evt.Info)
			require.True(t, proto.Equal(beforeMessage, evt.Message))

			check := func(step string) {
				got := readQuotedMedia(t, repo, orderDevice, orderChatJID, "ORIGINAL")
				require.Equal(t, "second edit", got.Content, step)
				edits, err := repo.GetMessageEdits("ORIGINAL", orderDevice)
				require.NoError(t, err)
				var history [][3]string
				for _, e := range edits {
					history = append(history, [3]string{e.EditEventID, e.PreviousContent, e.NewContent})
				}
				require.Equal(t, [][3]string{
					{"EDIT-1", "original", "first edit"},
					{"EDIT-2", "first edit", "second edit"},
				}, history, step)
				if tc.name == "caption_wrapped" {
					requireCopy(t, qmPhoto("original", nil), got.Copy)
					stored, err := repo.getMessageByDeviceAndChatIDAndMessageID(orderDevice, orderChatJID, "ORIGINAL")
					require.NoError(t, err)
					require.True(t, sent.Equal(stored.Timestamp), "%s: timestamp %v, want %v", step, stored.Timestamp, sent)
					require.Equal(t, "/v/photo", stored.DirectPath, step)
					require.Equal(t, []byte{1}, stored.MediaKey, step)
				}
			}
			check("edited")
			require.NoError(t, repo.CreateMessage(ctx, recovered("ORIGINAL", tc.original, sent, tc.requestID)))
			check("replayed")
		})
	}
}
