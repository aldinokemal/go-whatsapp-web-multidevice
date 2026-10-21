package chatstorage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

var qmThumb = []byte{0xFF, 0xD8, 0xFF, 0xE0}

// qmPhoto is a photo with its reply context (ci), as received or sent.
func qmPhoto(caption string, ci *waE2E.ContextInfo) *waE2E.Message {
	return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption: proto.String(caption), Mimetype: proto.String("image/jpeg"), JPEGThumbnail: qmThumb,
		MediaKey: []byte{1}, DirectPath: proto.String("/v/photo"), ContextInfo: ci,
	}}
}

func qmEncode(t *testing.T, msg *waE2E.Message) []byte {
	t.Helper()
	encoded, err := proto.Marshal(msg)
	require.NoError(t, err)
	return encoded
}

func qmRow(deviceID, chatJID, id, content string, quoted []byte) *domainChatStorage.Message {
	return &domainChatStorage.Message{ID: id, ChatJID: chatJID, DeviceID: deviceID, Sender: "550000000003@s.whatsapp.net",
		Content: content, IsFromMe: true, MediaType: "image", Timestamp: time.Now().UTC(), QuotedMedia: quoted}
}

func readQuotedMedia(t *testing.T, repo *SQLiteRepository, deviceID, chatJID, id string) *domainChatStorage.QuotedMedia {
	t.Helper()
	got, err := repo.GetQuotedMedia(context.Background(), deviceID, chatJID, id)
	require.NoError(t, err)
	return got
}

// requireCopy checks that a copy written by QuotedMediaCopy decodes to want.
func requireCopy(t *testing.T, want *waE2E.Message, encoded []byte) {
	t.Helper()
	decoded := &waE2E.Message{}
	require.NoError(t, proto.Unmarshal(encoded, decoded))
	require.True(t, proto.Equal(want, decoded), "copy %v, want %v", decoded, want)
}

func qmDeviceContext(deviceID string) context.Context {
	return whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(deviceID, nil, nil))
}

// A write without a copy (a replay, a row created by an edit) keeps the stored
// one, and so does an empty slice; the same ID on another device or in another
// chat keeps its own copy.
func TestQuotedMediaWritesKeepTheCopyWithinItsIdentity(t *testing.T) {
	const chat = "550000000002@s.whatsapp.net"
	for _, writer := range []string{"single", "batch", "sent"} {
		t.Run(writer, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			write := func(message *domainChatStorage.Message) {
				switch writer {
				case "batch":
					require.NoError(t, repo.StoreMessagesBatch([]*domainChatStorage.Message{message}))
				case "sent":
					require.NoError(t, repo.storeSentMessagePreservingEdits(message))
				default:
					require.NoError(t, repo.StoreMessage(message))
				}
			}
			a, b := qmEncode(t, qmPhoto("A", nil)), qmEncode(t, qmPhoto("B", nil))
			otherDevice, otherChat := qmEncode(t, qmPhoto("other device", nil)), qmEncode(t, qmPhoto("other chat", nil))
			write(qmRow("device-b", chat, "m1", "caption", otherDevice))
			write(qmRow("device-a", "120363000000000002@g.us", "m1", "caption", otherChat))

			for i, step := range []struct{ supplied, want []byte }{
				{nil, nil}, {a, a}, {nil, a}, {[]byte{}, a}, {b, b},
			} {
				write(qmRow("device-a", chat, "m1", "caption", step.supplied))
				got := readQuotedMedia(t, repo, "device-a", chat, "m1")
				require.NotNil(t, got)
				require.Equal(t, step.want, got.Copy, "step %d", i)
			}
			require.Equal(t, otherDevice, readQuotedMedia(t, repo, "device-b", chat, "m1").Copy)
			require.Equal(t, otherChat, readQuotedMedia(t, repo, "device-a", "120363000000000002@g.us", "m1").Copy)
			require.Nil(t, readQuotedMedia(t, repo, "device-c", chat, "m1"))
		})
	}
}

// GetQuotedMedia reads the copy with the row's current state, and counts only
// the edits of that row: the same ID edited on another device or in another
// chat does not count.
func TestGetQuotedMediaReadsTheRowAndItsEdits(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	ctx := qmDeviceContext(orderDevice)
	original := qmEncode(t, qmPhoto("before", nil))
	require.NoError(t, repo.StoreMessage(qmRow(orderDevice, orderChatJID, orderMsgID, "before", original)))
	for _, edit := range []*domainChatStorage.MessageEdit{
		{OriginalMessageID: orderMsgID, EditEventID: "E-DEVICE", ChatJID: orderChatJID, DeviceID: "device-2@s.whatsapp.net", NewContent: "x"},
		{OriginalMessageID: orderMsgID, EditEventID: "E-CHAT", ChatJID: "120363000000000002@g.us", DeviceID: orderDevice, NewContent: "x"},
	} {
		require.NoError(t, repo.StoreMessageEdit(edit))
	}

	got := readQuotedMedia(t, repo, orderDevice, orderChatJID, orderMsgID)
	require.Equal(t, &domainChatStorage.QuotedMedia{Copy: original, Content: "before", MediaType: "image", IsFromMe: true}, got)

	for _, content := range []string{"after", ""} {
		require.NoError(t, repo.CreateMessage(ctx, editEvent(orderMsgID, content, time.Now())))
		got = readQuotedMedia(t, repo, orderDevice, orderChatJID, orderMsgID)
		require.Equal(t, &domainChatStorage.QuotedMedia{Copy: original, Content: content, MediaType: "image", IsFromMe: true, HasEdits: true}, got)
	}
}

// GetQuotedMedia reads with the context it is given: once that context is
// cancelled, it reads nothing.
func TestGetQuotedMediaHonoursTheContext(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	const chat = "550000000002@s.whatsapp.net"
	require.NoError(t, repo.StoreMessage(qmRow("device-a", chat, "m1", "caption", qmEncode(t, qmPhoto("caption", nil)))))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := repo.GetQuotedMedia(ctx, "device-a", chat, "m1")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}

// The copy is read by GetQuotedMedia alone: message reads and listings leave
// it out, and so does the JSON of a message.
func TestQuotedMediaStaysOutOfMessageReads(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	const chat = "550000000002@s.whatsapp.net"
	require.NoError(t, repo.StoreMessage(qmRow("device-a", chat, "m1", "caption", qmEncode(t, qmPhoto("caption", nil)))))

	reads := map[string]func() (*domainChatStorage.Message, error){
		"global ID": func() (*domainChatStorage.Message, error) { return repo.GetMessageByID("m1") },
		"device ID": func() (*domainChatStorage.Message, error) { return repo.GetMessageByIDAndDevice("device-a", "m1") },
		"identity": func() (*domainChatStorage.Message, error) {
			return repo.GetMessageByIDChatAndDevice("device-a", chat, "m1")
		},
		"oldest": func() (*domainChatStorage.Message, error) { return repo.GetOldestMessageByDevice("device-a", chat) },
		"edit lookup": func() (*domainChatStorage.Message, error) {
			return repo.getMessageByDeviceAndChatIDAndMessageID("device-a", chat, "m1")
		},
	}
	for name, read := range reads {
		got, err := read()
		require.NoError(t, err, name)
		require.NotNil(t, got, name)
		require.Nil(t, got.QuotedMedia, name)
	}
	listed, err := repo.GetMessages(&domainChatStorage.MessageFilter{DeviceID: "device-a", ChatJID: chat, Limit: 10})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Nil(t, listed[0].QuotedMedia)
	found, err := repo.SearchMessages("device-a", chat, "caption", 10)
	require.NoError(t, err)
	require.Len(t, found, 1)
	require.Nil(t, found[0].QuotedMedia)

	encoded, err := json.Marshal(qmRow("device-a", chat, "m1", "caption", []byte{1, 2, 3}))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "QuotedMedia")
	require.NotContains(t, string(encoded), "quoted_media")
}

func legacyDatabase(t *testing.T) (*SQLiteRepository, []string) {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "legacy.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &SQLiteRepository{db: db}
	_, err = repo.getSchemaVersion()
	require.NoError(t, err)
	migrations := repo.getMigrations()
	for i, migration := range migrations[:49] {
		require.NoError(t, repo.runMigration(migration, i+1))
	}
	return repo, migrations
}

func quotedMediaColumns(t *testing.T, repo *SQLiteRepository) int {
	t.Helper()
	var columns int
	require.NoError(t, repo.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name = 'quoted_media'`).Scan(&columns))
	return columns
}

func TestQuotedMediaMigrationFromExistingDatabase(t *testing.T) {
	repo, migrations := legacyDatabase(t)
	_, err := repo.db.Exec(`INSERT INTO messages (id, chat_jid, device_id, sender, content, timestamp, is_from_me, media_type, filename)
		VALUES ('legacy', '550000000002@s.whatsapp.net', 'device-a', 'sender', 'legacy caption', ?, TRUE, 'image', '')`, time.Now().UTC())
	require.NoError(t, err)

	require.NoError(t, repo.InitializeSchema())
	require.NoError(t, repo.InitializeSchema(), "migration must be idempotent on restart")

	version, err := repo.getSchemaVersion()
	require.NoError(t, err)
	require.Equal(t, len(migrations), version)
	require.Equal(t, 1, quotedMediaColumns(t, repo))
	require.Equal(t, &domainChatStorage.QuotedMedia{Content: "legacy caption", MediaType: "image", IsFromMe: true},
		readQuotedMedia(t, repo, "device-a", "550000000002@s.whatsapp.net", "legacy"))

	copied := qmEncode(t, qmPhoto("legacy caption", nil))
	require.NoError(t, repo.StoreMessage(qmRow("device-a", "550000000002@s.whatsapp.net", "legacy", "legacy caption", copied)))
	require.Equal(t, copied, readQuotedMedia(t, repo, "device-a", "550000000002@s.whatsapp.net", "legacy").Copy)
}

// A quoted_media column created outside the migrations makes migration 50
// fail, and the schema stays at version 49.
func TestQuotedMediaMigrationFailureKeepsTheVersion(t *testing.T) {
	repo, _ := legacyDatabase(t)
	_, err := repo.db.Exec(`ALTER TABLE messages ADD COLUMN quoted_media BLOB`)
	require.NoError(t, err)

	require.ErrorContains(t, repo.InitializeSchema(), "failed to run migration 50")
	version, err := repo.getSchemaVersion()
	require.NoError(t, err)
	require.Equal(t, 49, version)
}

// The column stores a copy of the largest size kept byte for byte, and no copy
// as NULL, with the CGO and the purego driver alike.
func TestQuotedMediaColumnRoundTrip(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	const chat = "550000000002@s.whatsapp.net"
	largest := bytes.Repeat([]byte{0xAB}, 65535)
	require.NoError(t, repo.StoreMessage(qmRow("device-a", chat, "largest", "caption", largest)))
	require.NoError(t, repo.StoreMessage(qmRow("device-a", chat, "none", "caption", nil)))

	require.Equal(t, largest, readQuotedMedia(t, repo, "device-a", chat, "largest").Copy)
	require.Nil(t, readQuotedMedia(t, repo, "device-a", chat, "none").Copy)
	for id, want := range map[string]struct {
		kind   string
		length sql.NullInt64
	}{"largest": {"blob", sql.NullInt64{Int64: 65535, Valid: true}}, "none": {"null", sql.NullInt64{}}} {
		var kind string
		var length sql.NullInt64
		require.NoError(t, repo.db.QueryRow(`SELECT typeof(quoted_media), length(quoted_media) FROM messages WHERE id = ?`, id).Scan(&kind, &length))
		require.Equal(t, want.kind, kind, id)
		require.Equal(t, want.length, length, id)
	}
}

// The six media types as they arrive, with a reply context, and the copy kept.
func qmLiveMedia(ci *waE2E.ContextInfo) map[string]*waE2E.Message {
	return map[string]*waE2E.Message{
		"image": qmPhoto("photo", ci),
		"video": {VideoMessage: &waE2E.VideoMessage{Caption: proto.String("video"), Mimetype: proto.String("video/mp4"),
			GifPlayback: proto.Bool(true), Seconds: proto.Uint32(3), JPEGThumbnail: qmThumb, MediaKey: []byte{1}, ContextInfo: ci}},
		"video note": {PtvMessage: &waE2E.VideoMessage{Caption: proto.String("ptv"), Mimetype: proto.String("video/mp4"),
			Seconds: proto.Uint32(7), JPEGThumbnail: qmThumb, MediaKey: []byte{1}, ContextInfo: ci}},
		"voice note": {AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus"), PTT: proto.Bool(true),
			Seconds: proto.Uint32(12), MediaKey: []byte{1}, ContextInfo: ci}},
		"document": {DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String("doc"), FileName: proto.String("report.pdf"),
			Title: proto.String("report"), JPEGThumbnail: qmThumb, MediaKey: []byte{1}, ContextInfo: ci}},
		"sticker": {StickerMessage: &waE2E.StickerMessage{IsAnimated: proto.Bool(true), PngThumbnail: []byte{0x89},
			MediaKey: []byte{1}, ContextInfo: ci}},
	}
}

// Live messages, received or sent from another device of the account, keep
// their copy under their chat; in a group, the participant who sent it is not
// the chat.
func TestCreateMessageStoresTheQuotedMediaCopy(t *testing.T) {
	account := types.NewJID("550000000001", types.DefaultUserServer)
	contact := types.NewJID("550000000002", types.DefaultUserServer)
	group := types.NewJID("120363000000000001", types.GroupServer)
	participant := types.NewJID("550000000003", types.DefaultUserServer)
	reply := &waE2E.ContextInfo{StanzaID: proto.String("PARENT"), Participant: proto.String(participant.String())}
	repo := newTestSQLiteRepository(t) // each case stores its own message ID
	kept := qmLiveMedia(nil)
	for mediaName, msg := range qmLiveMedia(reply) {
		for _, source := range []struct {
			name         string
			chat, sender types.JID
			fromMe       bool
		}{
			{"received", contact, contact, false},
			{"from me", contact, account, true},
			{"group received", group, participant, false},
			{"group from me", group, account, true},
		} {
			t.Run(mediaName+"/"+source.name, func(t *testing.T) {
				id := mediaName + "/" + source.name
				require.NoError(t, repo.CreateMessage(qmDeviceContext("device-a"), &events.Message{
					Info: types.MessageInfo{ID: id, Timestamp: time.Now(), MessageSource: types.MessageSource{
						Chat: source.chat, Sender: source.sender, IsFromMe: source.fromMe, IsGroup: source.chat == group,
					}},
					Message: msg,
				}))

				got := readQuotedMedia(t, repo, "device-a", source.chat.String(), id)
				require.NotNil(t, got)
				requireCopy(t, kept[mediaName], got.Copy)
				if source.chat == group {
					require.Nil(t, readQuotedMedia(t, repo, "device-a", source.sender.String(), id))
				}
			})
		}
	}

	for name, tc := range map[string]struct {
		evt  *events.Message
		want *waE2E.Message
	}{
		"view-once": {&events.Message{Message: qmPhoto("photo", reply), IsViewOnce: true},
			&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), ViewOnce: proto.Bool(true)}}},
		"text": {&events.Message{Message: &waE2E.Message{Conversation: proto.String("hi")}}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			tc.evt.Info = types.MessageInfo{ID: name, Timestamp: time.Now(),
				MessageSource: types.MessageSource{Chat: contact, Sender: contact}}
			require.NoError(t, repo.CreateMessage(qmDeviceContext("device-a"), tc.evt))

			got := readQuotedMedia(t, repo, "device-a", contact.String(), name)
			require.NotNil(t, got)
			if tc.want == nil {
				require.Nil(t, got.Copy)
				return
			}
			requireCopy(t, tc.want, got.Copy)
		})
	}
}

// A live video note keeps its caption as content, next to its copy.
func TestVideoNoteCaptionIsStoredAsContent(t *testing.T) {
	note := &waE2E.Message{PtvMessage: &waE2E.VideoMessage{Caption: proto.String("ptv"), Seconds: proto.Uint32(7), JPEGThumbnail: qmThumb}}
	contact := types.NewJID("550000000002", types.DefaultUserServer)
	repo := newTestSQLiteRepository(t)
	require.NoError(t, repo.CreateMessage(qmDeviceContext("device-a"), &events.Message{
		Info:    types.MessageInfo{ID: "live", Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: contact, Sender: contact}},
		Message: note,
	}))
	got := readQuotedMedia(t, repo, "device-a", contact.String(), "live")
	require.NotNil(t, got)
	require.Equal(t, "ptv", got.Content)
	require.Equal(t, "video_note", got.MediaType)
	requireCopy(t, note, got.Copy)
}

// The copy of a sent message lands with its row whether an edit got there
// before or after it, and the edited content stays.
func TestStoreSentMessageStoresTheQuotedMediaCopy(t *testing.T) {
	for _, editFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "edit after", true: "edit before"}[editFirst], func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			ctx := qmDeviceContext(orderDevice)
			now := time.Now().UTC()
			store := func() {
				require.NoError(t, repo.StoreSentMessageWithContext(ctx, orderMsgID, orderDevice, orderChatJID, "before", now,
					qmPhoto("before", &waE2E.ContextInfo{StanzaID: proto.String("PARENT")})))
			}
			edit := func() {
				require.NoError(t, repo.CreateMessage(ctx, editEvent(orderMsgID, "edited", now.Add(time.Second))))
			}
			if editFirst {
				edit()
				store()
			} else {
				store()
				edit()
			}

			got := readQuotedMedia(t, repo, orderDevice, orderChatJID, orderMsgID)
			require.NotNil(t, got)
			require.Equal(t, "edited", got.Content)
			require.True(t, got.HasEdits)
			requireCopy(t, qmPhoto("before", nil), got.Copy)
		})
	}
}

// Once a message is edited, the original replayed by a live event, a history
// batch or a late sent store keeps the edited content, an empty one included;
// the copy stays the original's. Only edits of the message itself count.
func TestReplayAfterAnEditKeepsTheEditedContent(t *testing.T) {
	chat := types.NewJID("628123456789", types.DefaultUserServer)
	repo := newTestSQLiteRepository(t) // each case stores its own message ID
	ctx := qmDeviceContext(orderDevice)
	for _, replay := range []string{"live", "batch", "sent"} {
		for _, edited := range []string{"edited", ""} {
			name := replay + "/" + map[bool]string{true: "cleared", false: "edited"}[edited == ""]
			t.Run(name, func(t *testing.T) {
				now := time.Now().UTC()
				store := func() {
					switch replay {
					case "live":
						require.NoError(t, repo.CreateMessage(ctx, &events.Message{
							Info: types.MessageInfo{ID: name, Timestamp: now, MessageSource: types.MessageSource{
								Chat: chat, Sender: chat, IsFromMe: true,
							}},
							Message: qmPhoto("before", nil),
						}))
					case "batch":
						require.NoError(t, repo.StoreMessagesBatch([]*domainChatStorage.Message{
							qmRow(orderDevice, orderChatJID, name, "before", qmEncode(t, qmPhoto("before", nil))),
						}))
					default:
						require.NoError(t, repo.StoreSentMessageWithContext(ctx, name, orderDevice, orderChatJID, "before", now, qmPhoto("before", nil)))
					}
				}
				store()
				require.NoError(t, repo.CreateMessage(ctx, editEvent(name, edited, now.Add(time.Second))))
				store()

				got := readQuotedMedia(t, repo, orderDevice, orderChatJID, name)
				require.NotNil(t, got)
				require.Equal(t, edited, got.Content)
				require.True(t, got.HasEdits)
				requireCopy(t, qmPhoto("before", nil), got.Copy)
			})
		}
	}

	// The same ID edited on another device or in another chat: a replay of
	// this message, never edited, still updates its content.
	for _, writer := range []string{"single", "batch"} {
		t.Run("not edited/"+writer, func(t *testing.T) {
			id := "not edited/" + writer
			for _, edit := range []*domainChatStorage.MessageEdit{
				{OriginalMessageID: id, EditEventID: "E-DEVICE", ChatJID: orderChatJID, DeviceID: "device-2@s.whatsapp.net", NewContent: "x"},
				{OriginalMessageID: id, EditEventID: "E-CHAT", ChatJID: "120363000000000002@g.us", DeviceID: orderDevice, NewContent: "x"},
			} {
				require.NoError(t, repo.StoreMessageEdit(edit))
			}
			for _, content := range []string{"first", "second"} {
				row := qmRow(orderDevice, orderChatJID, id, content, nil)
				if writer == "batch" {
					require.NoError(t, repo.StoreMessagesBatch([]*domainChatStorage.Message{row}))
				} else {
					require.NoError(t, repo.StoreMessage(row))
				}
			}
			require.Equal(t, "second", readQuotedMedia(t, repo, orderDevice, orderChatJID, id).Content)
		})
	}
}

// A newsletter edit arrives as the new content under the original ID, not as
// a MESSAGE_EDIT: it replaces the content even after a recorded edit, and a
// replay of the original after it, with newsletter metadata but no edit time,
// still keeps the edited content.
func TestNewsletterEditAfterAnEditReplacesTheContent(t *testing.T) {
	chat := types.NewJID("628123456789", types.DefaultUserServer)
	repo := newTestSQLiteRepository(t)
	ctx := qmDeviceContext(orderDevice)
	const id = "newsletter-edit"
	now := time.Now().UTC()
	live := func(caption string, at time.Time, meta *events.NewsletterMessageMeta) {
		require.NoError(t, repo.CreateMessage(ctx, &events.Message{
			Info: types.MessageInfo{ID: id, Timestamp: at, MessageSource: types.MessageSource{
				Chat: chat, Sender: chat, IsFromMe: true,
			}},
			Message:        qmPhoto(caption, nil),
			NewsletterMeta: meta,
		}))
	}

	original := &events.NewsletterMessageMeta{OriginalTS: now}

	live("before", now, original)
	require.NoError(t, repo.CreateMessage(ctx, editEvent(id, "A", now.Add(time.Second))))
	live("B", now, &events.NewsletterMessageMeta{EditTS: now.Add(2 * time.Second), OriginalTS: now})

	got := readQuotedMedia(t, repo, orderDevice, orderChatJID, id)
	require.Equal(t, "B", got.Content)
	require.True(t, got.HasEdits)
	requireCopy(t, qmPhoto("B", nil), got.Copy)

	live("before", now, original)
	got = readQuotedMedia(t, repo, orderDevice, orderChatJID, id)
	require.Equal(t, "B", got.Content)
	require.True(t, got.HasEdits)
}

// History copies are written by the batch statements, so a batch that fails
// keeps neither its messages nor their copies.
func TestStoreMessagesBatchFailureKeepsTheStoredCopies(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	const chat = "550000000002@s.whatsapp.net"
	before, after := qmEncode(t, qmPhoto("before", nil)), qmEncode(t, qmPhoto("after", nil))
	require.NoError(t, repo.StoreMessage(qmRow("device-a", chat, "m1", "before", before)))
	_, err := repo.db.Exec(`CREATE TRIGGER refuse_m2 BEFORE INSERT ON messages WHEN NEW.id = 'm2'
		BEGIN SELECT RAISE(ABORT, 'm2 refused'); END`)
	require.NoError(t, err)

	require.Error(t, repo.StoreMessagesBatch([]*domainChatStorage.Message{
		qmRow("device-a", chat, "m1", "after", after),
		qmRow("device-a", chat, "m2", "new", qmEncode(t, qmPhoto("new", nil))),
	}))

	require.Equal(t, &domainChatStorage.QuotedMedia{Copy: before, Content: "before", MediaType: "image", IsFromMe: true},
		readQuotedMedia(t, repo, "device-a", chat, "m1"))
	require.Nil(t, readQuotedMedia(t, repo, "device-a", chat, "m2"))
}

// expiringContext reports its deadline as passed from its second check on.
// StoreSentMessageWithContext checks it before the message write and again
// before the chat write, so the deadline falls between the two.
type expiringContext struct {
	context.Context
	checks int
}

func (c *expiringContext) Done() <-chan struct{} {
	c.checks++
	if c.checks < 2 {
		return nil
	}
	done := make(chan struct{})
	close(done)
	return done
}

func (c *expiringContext) Err() error {
	if c.checks < 2 {
		return nil
	}
	return context.DeadlineExceeded
}

// The copy goes in with the message: a deadline that passes after the message
// write skips only the chat update, and a cancelled store writes nothing.
func TestStoreSentMessageWritesTheCopyWithTheMessage(t *testing.T) {
	sent := qmPhoto("caption", nil)
	t.Run("cancelled before", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		ctx, cancel := context.WithCancel(qmDeviceContext(orderDevice))
		cancel()
		err := repo.StoreSentMessageWithContext(ctx, orderMsgID, orderDevice, orderChatJID, "caption", time.Now(), sent)
		require.ErrorIs(t, err, context.Canceled)
		require.Nil(t, readQuotedMedia(t, repo, orderDevice, orderChatJID, orderMsgID))
	})
	t.Run("deadline after the message", func(t *testing.T) {
		repo := newTestSQLiteRepository(t)
		ctx := whatsapp.ContextWithDevice(&expiringContext{Context: context.Background()}, whatsapp.NewDeviceInstance(orderDevice, nil, nil))
		err := repo.StoreSentMessageWithContext(ctx, orderMsgID, orderDevice, orderChatJID, "caption", time.Now(), sent)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		got := readQuotedMedia(t, repo, orderDevice, orderChatJID, orderMsgID)
		require.NotNil(t, got)
		require.Equal(t, "caption", got.Content)
		requireCopy(t, sent, got.Copy)
		chat, err := repo.GetChatByDevice(orderDevice, orderChatJID)
		require.NoError(t, err)
		require.Nil(t, chat)
	})
}

// The copy goes with its row: once the message is revoked or deleted, a row
// stored again under its identity has no copy, while the same ID elsewhere
// keeps its own unless the operation covers it.
func TestQuotedMediaGoesWithItsMessage(t *testing.T) {
	const (
		chat      = "550000000002@s.whatsapp.net"
		otherChat = "120363000000000002@g.us"
	)
	type key struct{ device, chat string }
	target, otherDevice, inOtherChat := key{"device-a", chat}, key{"device-b", chat}, key{"device-a", otherChat}
	for name, tc := range map[string]struct {
		remove func(*SQLiteRepository) error
		kept   []key
	}{
		"revoke": {func(r *SQLiteRepository) error {
			jid := types.NewJID("550000000002", types.DefaultUserServer)
			return r.CreateMessage(qmDeviceContext("device-a"), &events.Message{
				Info: types.MessageInfo{ID: "REVOKE-1", Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: jid, Sender: jid}},
				Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
					Type: waE2E.ProtocolMessage_REVOKE.Enum(), Key: &waCommon.MessageKey{ID: proto.String("m1")},
				}},
			})
		}, []key{otherDevice, inOtherChat}},
		"message of a device": {func(r *SQLiteRepository) error { return r.DeleteMessageByDevice("device-a", "m1", chat) }, []key{otherDevice, inOtherChat}},
		"message":             {func(r *SQLiteRepository) error { return r.DeleteMessage("m1", chat) }, []key{inOtherChat}},
		"chat of a device":    {func(r *SQLiteRepository) error { return r.DeleteChatByDevice("device-a", chat) }, []key{otherDevice, inOtherChat}},
		"chat":                {func(r *SQLiteRepository) error { return r.DeleteChat(chat) }, []key{inOtherChat}},
		"device":              {func(r *SQLiteRepository) error { return r.DeleteDeviceData("device-a") }, []key{otherDevice}},
		"truncate":            {func(r *SQLiteRepository) error { return r.TruncateAllChats() }, nil},
	} {
		t.Run(name, func(t *testing.T) {
			repo := newTestSQLiteRepository(t)
			copies := map[key][]byte{}
			for _, k := range []key{target, otherDevice, inOtherChat} {
				copies[k] = qmEncode(t, qmPhoto(k.device+" "+k.chat, nil))
				require.NoError(t, repo.StoreMessage(qmRow(k.device, k.chat, "m1", "caption", copies[k])))
			}

			require.NoError(t, tc.remove(repo))
			require.NoError(t, repo.StoreMessage(qmRow(target.device, target.chat, "m1", "caption", nil)))

			require.Nil(t, readQuotedMedia(t, repo, target.device, target.chat, "m1").Copy)
			for _, k := range []key{otherDevice, inOtherChat} {
				got := readQuotedMedia(t, repo, k.device, k.chat, "m1")
				if !slices.Contains(tc.kept, k) {
					require.Nil(t, got, "%v", k)
					continue
				}
				require.NotNil(t, got, "%v", k)
				require.Equal(t, copies[k], got.Copy, "%v", k)
			}
		})
	}
}
