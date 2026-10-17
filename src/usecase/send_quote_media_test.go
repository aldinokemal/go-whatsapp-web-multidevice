package usecase

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// The quoted message is a participant's message in a group: the copy must be
// read by the row's identity, never by its sender.
const (
	quoteDevice = "550000000001@s.whatsapp.net"
	quoteChat   = "120363000000000001@g.us"
	quoteSender = "550000000003@s.whatsapp.net"
	quoteID     = "3EB0QUOTEDMEDIA"
)

var (
	quoteGroup = types.NewJID("120363000000000001", types.GroupServer)
	quoteThumb = []byte{0xFF, 0xD8, 0xFF, 0xE0}
)

// quoteMediaRepo serves one stored row and its quote state, and records the
// GetQuotedMedia reads.
type quoteMediaRepo struct {
	replyMessageRepo
	quoted     *domainChatStorage.QuotedMedia
	quotedErr  error
	quoteCtx   context.Context
	quoteReads [][]string // device, chat and ID of each read
}

func (r *quoteMediaRepo) GetQuotedMedia(ctx context.Context, deviceID, chatJID, id string) (*domainChatStorage.QuotedMedia, error) {
	r.quoteCtx = ctx
	r.quoteReads = append(r.quoteReads, []string{deviceID, chatJID, id})
	return r.quoted, r.quotedErr
}

func quoteRow(mediaType, content string) *domainChatStorage.Message {
	return &domainChatStorage.Message{ID: quoteID, ChatJID: quoteChat, DeviceID: quoteDevice, Sender: quoteSender,
		Content: content, MediaType: mediaType, MediaKey: []byte{1}, DirectPath: "/v/file"}
}

// quoteRepo serves row, with stored (nil: none) as its copy in the quote state
// GetQuotedMedia reads back; cases adjust that state further.
func quoteRepo(t *testing.T, row *domainChatStorage.Message, stored *waE2E.Message) *quoteMediaRepo {
	t.Helper()
	quoted := &domainChatStorage.QuotedMedia{Content: row.Content, MediaType: row.MediaType, IsFromMe: row.IsFromMe}
	if stored != nil {
		quoted.Copy = quoteEncode(t, stored)
	}
	return &quoteMediaRepo{replyMessageRepo: replyMessageRepo{message: row}, quoted: quoted}
}

func quoteEncode(t *testing.T, m *waE2E.Message) []byte {
	t.Helper()
	encoded, err := proto.Marshal(m)
	require.NoError(t, err)
	return encoded
}

func quoteContext(client *whatsmeow.Client) context.Context {
	return whatsapp.ContextWithDevice(context.Background(), whatsapp.NewDeviceInstance(quoteDevice, client, nil))
}

// quoteTo replies to quoteID from quoteDevice, sent to recipient, and returns
// the quoted message once the quote's ID and sender are checked.
func quoteTo(t *testing.T, repo *quoteMediaRepo, recipient types.JID) *waE2E.Message {
	t.Helper()
	return quoteToWith(t, quoteContext(nil), repo, recipient)
}

func quoteToWith(t *testing.T, ctx context.Context, repo domainChatStorage.IChatStorageRepository, recipient types.JID) *waE2E.Message {
	t.Helper()
	replyID := quoteID
	got := serviceSend{chatStorageRepo: repo}.mergeReplyContext(ctx, &waE2E.ContextInfo{}, &replyID, recipient)
	require.Equal(t, quoteID, got.GetStanzaID())
	require.Equal(t, quoteSender, got.GetParticipant())
	return got.GetQuotedMessage()
}

func requireQuoted(t *testing.T, want, got *waE2E.Message) {
	t.Helper()
	require.True(t, proto.Equal(want, got), "quoted %v, want %v", got, want)
}

// quoteFixture is the stored copy, and the quote, of each media type: the
// media message with every field a quote shows, captioned like the row.
func quoteFixture(mediaType string) *waE2E.Message {
	switch mediaType {
	case "image":
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("photo"), Mimetype: proto.String("image/jpeg"),
			JPEGThumbnail: quoteThumb, Width: proto.Uint32(640), Height: proto.Uint32(480), MediaKey: []byte{1}, DirectPath: proto.String("/v/image")}}
	case "video":
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String("clip"), Mimetype: proto.String("video/mp4"),
			GifPlayback: proto.Bool(true), Seconds: proto.Uint32(3), JPEGThumbnail: quoteThumb, MediaKey: []byte{1}}}
	case "video_note":
		return &waE2E.Message{PtvMessage: &waE2E.VideoMessage{Caption: proto.String("ptv"), Mimetype: proto.String("video/mp4"),
			Seconds: proto.Uint32(7), JPEGThumbnail: quoteThumb, MediaKey: []byte{1}}}
	case "audio":
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus"), PTT: proto.Bool(true),
			Seconds: proto.Uint32(12), Waveform: []byte{4, 5, 6}, MediaKey: []byte{1}}}
	case "document":
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String("notes"), Mimetype: proto.String("application/pdf"),
			FileName: proto.String("report.pdf"), Title: proto.String("Report"), JPEGThumbnail: quoteThumb, MediaKey: []byte{1}}}
	case "sticker":
		return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{Mimetype: proto.String("image/webp"), IsAnimated: proto.Bool(true),
			PngThumbnail: []byte{137, 80, 78, 71}, MediaKey: []byte{1}}}
	}
	return nil
}

// quoteRowContent is what a received message of the fixture stores as content.
var quoteRowContent = map[string]string{"image": "photo", "video": "clip", "video_note": "ptv", "audio": "", "document": "notes", "sticker": ""}

// The stored copy is quoted whole (thumbnail, mimetype, duration, PTT...),
// read by the row's identity.
func TestMergeReplyContextQuotesTheStoredMediaCopy(t *testing.T) {
	for mediaType, content := range quoteRowContent {
		t.Run(mediaType, func(t *testing.T) {
			repo := quoteRepo(t, quoteRow(mediaType, content), quoteFixture(mediaType))

			requireQuoted(t, quoteFixture(mediaType), quoteTo(t, repo, quoteGroup))
			require.Equal(t, [][]string{{quoteDevice, quoteChat, quoteID}}, repo.quoteReads)
		})
	}
	t.Run("view-once", func(t *testing.T) {
		viewOnce := func() *waE2E.Message {
			return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), ViewOnce: proto.Bool(true)}}
		}
		repo := quoteRepo(t, quoteRow("image", "secret"), viewOnce())

		requireQuoted(t, viewOnce(), quoteTo(t, repo, quoteGroup))
	})
	t.Run("request context", func(t *testing.T) {
		repo := quoteRepo(t, quoteRow("image", "photo"), quoteFixture("image"))
		ctx := context.WithValue(quoteContext(nil), quoteContextKey{}, "request")

		quoteToWith(t, ctx, repo, quoteGroup)
		require.Equal(t, ctx, repo.quoteCtx)
	})
}

type quoteContextKey struct{}

// quoteFiller is a media message of mediaType whose bulk is filler.
func quoteFiller(mediaType string, filler []byte) *waE2E.Message {
	switch mediaType {
	case "image":
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{JPEGThumbnail: filler}}
	case "video":
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{JPEGThumbnail: filler}}
	case "video_note":
		return &waE2E.Message{PtvMessage: &waE2E.VideoMessage{JPEGThumbnail: filler}}
	case "audio":
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{Waveform: filler}}
	case "document":
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{JPEGThumbnail: filler}}
	}
	return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{PngThumbnail: filler}}
}

// Media whose copy is missing, unreadable or not a lone copy of its type is
// quoted by its type alone: the row cannot tell whether it was view-once, so
// no caption, file name, thumbnail or keys go with it.
func TestMergeReplyContextQuotesMediaByTypeWithoutAUsableCopy(t *testing.T) {
	typed := map[string]*waE2E.Message{
		"image":      {ImageMessage: &waE2E.ImageMessage{}},
		"video":      {VideoMessage: &waE2E.VideoMessage{}},
		"video_note": {PtvMessage: &waE2E.VideoMessage{}},
		"audio":      {AudioMessage: &waE2E.AudioMessage{}},
		"document":   {DocumentMessage: &waE2E.DocumentMessage{}},
		"sticker":    {StickerMessage: &waE2E.StickerMessage{}},
	}
	for mediaType, want := range typed {
		other := "image"
		if mediaType == "image" {
			other = "video"
		}
		withText := quoteFiller(mediaType, quoteThumb)
		withText.Conversation = proto.String("hidden text")
		for name, tc := range map[string]struct {
			copy []byte
			err  error
		}{
			"no copy":    {},
			"read error": {quoteEncode(t, quoteFixture(mediaType)), errors.New("database is locked")},
			"unreadable": {[]byte{0xFF, 0xFF, 0xFF}, nil},
			// A copy cut short: it decodes up to the media message, then fails.
			"truncated":       {append(quoteEncode(t, quoteFixture(mediaType)), 0x0A), nil},
			"oversized":       {quoteEncode(t, quoteFiller(mediaType, bytes.Repeat([]byte{0xAB}, 64<<10))), nil},
			"another type":    {quoteEncode(t, quoteFixture(other)), nil},
			"text only":       {quoteEncode(t, &waE2E.Message{Conversation: proto.String("hidden text")}), nil},
			"with other text": {quoteEncode(t, withText), nil},
		} {
			t.Run(mediaType+"/"+name, func(t *testing.T) {
				repo := quoteRepo(t, quoteRow(mediaType, "see attached"), nil)
				repo.quoted.Copy = tc.copy
				repo.quotedErr = tc.err

				requireQuoted(t, want, quoteTo(t, repo, quoteGroup))
				require.Equal(t, [][]string{{quoteDevice, quoteChat, quoteID}}, repo.quoteReads)
			})
		}
	}
}

// A stored copy is read only under 64 KiB, the bound it is written with.
func TestMergeReplyContextReadsCopiesUnder64KiB(t *testing.T) {
	sized := func(size int) *waE2E.Message {
		for n := size; n > 0; n-- {
			msg := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{JPEGThumbnail: bytes.Repeat([]byte{0xAB}, n)}}
			if proto.Size(msg) == size {
				return msg
			}
		}
		t.Fatalf("no message of %d bytes", size)
		return nil
	}
	for size, want := range map[int]*waE2E.Message{
		65535: sized(65535),
		65536: {ImageMessage: &waE2E.ImageMessage{}},
	} {
		repo := quoteRepo(t, quoteRow("image", ""), sized(size))

		requireQuoted(t, want, quoteTo(t, repo, quoteGroup))
	}
}

// A copy is quoted as a new copy would be stored: without a reply context of
// its own, and with only the type of view-once media.
func TestMergeReplyContextReducesTheStoredCopy(t *testing.T) {
	extras := func(m *waE2E.ImageMessage) *waE2E.ImageMessage {
		m.Caption, m.JPEGThumbnail, m.MediaKey = proto.String("secret"), quoteThumb, []byte{9}
		m.ContextInfo = &waE2E.ContextInfo{StanzaID: proto.String("OLD-PARENT")}
		return m
	}
	for name, tc := range map[string]struct {
		mediaType   string
		copy, quote *waE2E.Message
	}{
		"reply context": {"image",
			&waE2E.Message{ImageMessage: extras(&waE2E.ImageMessage{Mimetype: proto.String("image/jpeg")})},
			&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("secret"), Mimetype: proto.String("image/jpeg"),
				JPEGThumbnail: quoteThumb, MediaKey: []byte{9}}}},
		"view-once image": {"image",
			&waE2E.Message{ImageMessage: extras(&waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), ViewOnce: proto.Bool(true)})},
			&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), ViewOnce: proto.Bool(true)}}},
		"view-once video": {"video",
			&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Mimetype: proto.String("video/mp4"), Caption: proto.String("secret"),
				JPEGThumbnail: quoteThumb, ViewOnce: proto.Bool(true)}},
			&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Mimetype: proto.String("video/mp4"), ViewOnce: proto.Bool(true)}}},
		"view-once video note": {"video_note",
			&waE2E.Message{PtvMessage: &waE2E.VideoMessage{Mimetype: proto.String("video/mp4"), Caption: proto.String("secret"),
				JPEGThumbnail: quoteThumb, ViewOnce: proto.Bool(true)}},
			&waE2E.Message{PtvMessage: &waE2E.VideoMessage{Mimetype: proto.String("video/mp4"), ViewOnce: proto.Bool(true)}}},
		"view-once voice note": {"audio",
			&waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg"), PTT: proto.Bool(true),
				Waveform: []byte{4}, MediaKey: []byte{9}, ViewOnce: proto.Bool(true)}},
			&waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg"), PTT: proto.Bool(true), ViewOnce: proto.Bool(true)}}},
	} {
		t.Run(name, func(t *testing.T) {
			repo := quoteRepo(t, quoteRow(tc.mediaType, "secret"), tc.copy)

			requireQuoted(t, tc.quote, quoteTo(t, repo, quoteGroup))
		})
	}
}

// quoteLIDStore maps one @lid to its phone JID. Looking up quoteFailingLID
// fails, though it returns that phone JID too.
type quoteLIDStore struct {
	waStore.LIDStore
	lid, pn types.JID
}

func (s *quoteLIDStore) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	switch lid.ToNonAD() {
	case s.lid:
		return s.pn, nil
	case quoteFailingLID:
		return s.pn, errors.New("lid store unavailable")
	}
	return types.EmptyJID, nil
}

var (
	quoteContact    = types.NewJID("550000000002", types.DefaultUserServer)
	quoteLID        = types.NewJID("88000000000002", types.HiddenUserServer)
	quoteFailingLID = types.NewJID("88000000000005", types.HiddenUserServer)
)

// quoteClient maps quoteLID to quoteContact, as the sending device does.
func quoteClient() *whatsmeow.Client {
	device := types.NewJID("550000000001", types.DefaultUserServer)
	return &whatsmeow.Client{Store: &waStore.Device{ID: &device, LIDs: &quoteLIDStore{lid: quoteLID, pn: quoteContact}}}
}

// The copy holds the media's download keys, so media is quoted as media only
// in its own chat. The chat matches whatever device suffix either JID
// carries, and an @lid on either side mapped to the phone JID of the other;
// the copy is still read by the chat the row is stored under. A reply in
// another chat, or to a message that is not media, keeps the plain-text quote
// and reads no copy, and so does a status or broadcast list: a reply posted
// there reaches another audience than the quoted message did. Matching the
// chats logs nothing, at any level.
func TestMergeReplyContextQuotesMediaOnlyInItsChat(t *testing.T) {
	for name, tc := range map[string]struct {
		chat      string
		recipient types.JID
	}{
		"recipient with a device suffix":   {quoteContact.String(), types.NewADJID("550000000002", 0, 3)},
		"stored chat with a device suffix": {"550000000002:3@s.whatsapp.net", quoteContact},
		"recipient @lid":                   {quoteContact.String(), quoteLID},
		"stored @lid":                      {quoteLID.String(), quoteContact},
	} {
		t.Run(name, func(t *testing.T) {
			logs := quoteLogs(t, logrus.TraceLevel)
			row := quoteRow("image", "photo")
			row.ChatJID = tc.chat
			repo := quoteRepo(t, row, quoteFixture("image"))

			requireQuoted(t, quoteFixture("image"), quoteToWith(t, quoteContext(quoteClient()), repo, tc.recipient))
			require.Equal(t, [][]string{{quoteDevice, tc.chat, quoteID}}, repo.quoteReads)
			require.Empty(t, logs.String())
		})
	}

	client := quoteClient()
	unmapped := types.NewJID("88000000000009", types.HiddenUserServer)
	list := types.NewJID("1700000000", types.BroadcastServer)
	for name, tc := range map[string]struct {
		mediaType, chat string
		recipient       types.JID
		client          *whatsmeow.Client
	}{
		"another contact":      {"image", quoteChat, types.NewJID("550000000009", types.DefaultUserServer), client},
		"another group":        {"image", quoteChat, types.NewJID("120363000000000002", types.GroupServer), client},
		"no recipient":         {"image", quoteChat, types.EmptyJID, client},
		"unmapped @lid":        {"image", quoteContact.String(), unmapped, client},
		"unmapped stored @lid": {"image", unmapped.String(), quoteContact, client},
		"two unmapped @lids":   {"image", unmapped.String(), types.NewJID("88000000000008", types.HiddenUserServer), client},
		"failed @lid lookup":   {"image", quoteFailingLID.String(), quoteContact, client},
		"no client":            {"image", quoteLID.String(), quoteContact, nil},
		"status":               {"image", types.StatusBroadcastJID.String(), types.StatusBroadcastJID, client},
		"broadcast list":       {"image", list.String(), list, client},
		"reply to a status":    {"image", types.StatusBroadcastJID.String(), quoteContact, client},
		"status quoting chat":  {"image", quoteContact.String(), types.StatusBroadcastJID, client},
		"text":                 {"", quoteChat, quoteGroup, client},
		"call":                 {"call", quoteChat, quoteGroup, client},
	} {
		t.Run("text quote/"+name, func(t *testing.T) {
			logs := quoteLogs(t, logrus.TraceLevel)
			row := quoteRow(tc.mediaType, "body")
			row.ChatJID = tc.chat
			repo := quoteRepo(t, row, quoteFixture("image"))

			requireQuoted(t, &waE2E.Message{Conversation: proto.String("body")}, quoteToWith(t, quoteContext(tc.client), repo, tc.recipient))
			require.Empty(t, repo.quoteReads)
			require.Empty(t, logs.String())
		})
	}
}

// The quote adds its own fields to the reply's context and leaves the rest.
func TestMergeReplyContextKeepsTheRestOfTheContext(t *testing.T) {
	repo := quoteRepo(t, quoteRow("image", "photo"), quoteFixture("image"))
	contextInfo := &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(100),
		Expiration: proto.Uint32(3600), MentionedJID: []string{"550000000009@s.whatsapp.net"}}
	replyID := quoteID

	got := serviceSend{chatStorageRepo: repo}.mergeReplyContext(quoteContext(nil), contextInfo, &replyID, quoteGroup)

	require.Same(t, contextInfo, got)
	require.True(t, proto.Equal(&waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(100),
		Expiration: proto.Uint32(3600), MentionedJID: []string{"550000000009@s.whatsapp.net"},
		StanzaID: proto.String(quoteID), Participant: proto.String(quoteSender), QuotedMessage: quoteFixture("image")}, got), "context %v", got)
}

// A message removed between the lookup and the read of its copy is not
// quoted: the reply goes out with its context as it was.
func TestMergeReplyContextLeavesTheContextWhenTheMessageIsGone(t *testing.T) {
	repo := quoteRepo(t, quoteRow("image", "photo"), quoteFixture("image"))
	repo.quoted = nil
	replyID := quoteID
	contextInfo := &waE2E.ContextInfo{Expiration: proto.Uint32(3600)}

	got := serviceSend{chatStorageRepo: repo}.mergeReplyContext(quoteContext(nil), contextInfo, &replyID, quoteGroup)

	require.Same(t, contextInfo, got)
	require.True(t, proto.Equal(&waE2E.ContextInfo{Expiration: proto.Uint32(3600)}, got), "context %v", got)
	require.Nil(t, serviceSend{chatStorageRepo: repo}.mergeReplyContext(quoteContext(nil), nil, &replyID, quoteGroup))
}

// A row that is no longer media when its copy is read is quoted as text.
func TestMergeReplyContextQuotesTextWhenTheRowIsNoLongerMedia(t *testing.T) {
	repo := quoteRepo(t, quoteRow("image", "photo"), nil)
	repo.quoted = &domainChatStorage.QuotedMedia{Content: "now text"}

	requireQuoted(t, &waE2E.Message{Conversation: proto.String("now text")}, quoteTo(t, repo, quoteGroup))
}

// quoteLogs captures what the standard logger writes at level, until the test
// ends.
func quoteLogs(t *testing.T, level logrus.Level) *bytes.Buffer {
	t.Helper()
	logger := logrus.StandardLogger()
	logs := &bytes.Buffer{}
	previousOut, previousFormatter, previousLevel := logger.Out, logger.Formatter, logger.GetLevel()
	logger.SetOutput(logs)
	logger.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logger.SetLevel(level)
	t.Cleanup(func() {
		logger.SetOutput(previousOut)
		logger.SetFormatter(previousFormatter)
		logger.SetLevel(previousLevel)
	})
	return logs
}

// The read error may carry message data: the log names the failure alone.
func TestMergeReplyContextLogsNoMessageDataWhenTheCopyCannotBeRead(t *testing.T) {
	logs := quoteLogs(t, logrus.InfoLevel)
	repo := quoteRepo(t, quoteRow("image", "secret caption"), nil)
	repo.quotedErr = errors.New("reading secret caption of 550000000003@s.whatsapp.net failed")

	requireQuoted(t, &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}}, quoteTo(t, repo, quoteGroup))

	require.Equal(t, "level=warning msg=\"Quoting media by its type only: its stored copy could not be read\"\n", logs.String())
	require.NotContains(t, logs.String(), "secret caption")
	require.NotContains(t, logs.String(), "550000000003")
}

// The caption of a quoted copy is the row's content, edits included. Only an
// unedited message this API sent swaps the summary it stores for the
// caption of the copy.
func TestMergeReplyContextCaptions(t *testing.T) {
	media := func(mediaType, caption, fileName string) *waE2E.Message {
		switch mediaType {
		case "video":
			return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{Caption: proto.String(caption), JPEGThumbnail: quoteThumb}}
		case "video_note":
			return &waE2E.Message{PtvMessage: &waE2E.VideoMessage{Caption: proto.String(caption), JPEGThumbnail: quoteThumb}}
		case "document":
			return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String(caption), FileName: proto.String(fileName), JPEGThumbnail: quoteThumb}}
		}
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String(caption), JPEGThumbnail: quoteThumb}}
	}
	for _, tc := range []struct {
		name                         string
		mediaType, copyCaption, file string
		content                      string
		fromMe, edited               bool
		caption                      *string
	}{
		{"sent image without caption", "image", "", "", "🖼️ Image", true, false, nil},
		{"sent image captioned like its summary", "image", "🖼️ Image", "", "🖼️ Image", true, false, proto.String("🖼️ Image")},
		{"received image captioned like a summary", "image", "", "", "🖼️ Image", false, false, proto.String("🖼️ Image")},
		{"sent video without caption", "video", "", "", "🎥 Video", true, false, nil},
		{"sent video captioned Video", "video", "Video", "", "🎥 Video", true, false, proto.String("Video")},
		{"sent video", "video", "obra", "", "🎥 obra", true, false, proto.String("obra")},
		{"sent video captioned like its summary", "video", "🎥 obra", "", "🎥 🎥 obra", true, false, proto.String("🎥 obra")},
		{"video from the phone", "video", "🎥 obra", "", "🎥 obra", true, false, proto.String("🎥 obra")},
		{"edited to a video summary", "video", "obra", "", "🎥 nova", true, true, proto.String("🎥 nova")},
		{"edited to the summary of its caption", "video", "obra", "", "🎥 obra", true, true, proto.String("🎥 obra")},
		{"edited to an image summary", "image", "", "", "🖼️ Image", true, true, proto.String("🖼️ Image")},
		{"edited to a document summary", "document", "", "report.pdf", "📄 report.pdf", true, true, proto.String("📄 report.pdf")},
		{"sent document without caption", "document", "", "report.pdf", "📄 report.pdf", true, false, nil},
		{"sent document without name", "document", "", "", "📄 Document", true, false, nil},
		{"document captioned like its summary", "document", "📄 report.pdf", "report.pdf", "📄 report.pdf", true, false, proto.String("📄 report.pdf")},
		{"forwarded video without caption", "video", "", "", "🎬 Video", true, false, nil},
		{"forwarded video note without caption", "video_note", "", "", "🎬 Video", true, false, nil},
		{"forwarded document without caption", "document", "", "report.pdf", "📄 Document", true, false, nil},
		{"edited caption", "image", "before", "", "after", true, true, proto.String("after")},
		{"edited video note", "video_note", "ptv", "", "edited ptv", false, true, proto.String("edited ptv")},
		{"caption edited away", "image", "before", "", "", true, true, nil},
		{"received document", "document", "notes", "report.pdf", "notes", false, false, proto.String("notes")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := quoteRow(tc.mediaType, tc.content)
			row.IsFromMe = tc.fromMe
			repo := quoteRepo(t, row, media(tc.mediaType, tc.copyCaption, tc.file))
			repo.quoted.HasEdits = tc.edited

			requireQuoted(t, quoteCaptioned(media(tc.mediaType, tc.copyCaption, tc.file), tc.caption), quoteTo(t, repo, quoteGroup))
		})
	}

	t.Run("view-once", func(t *testing.T) {
		for _, edited := range []bool{false, true} {
			repo := quoteRepo(t, quoteRow("image", "secret"),
				&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), ViewOnce: proto.Bool(true)}})
			repo.quoted.HasEdits = edited

			requireQuoted(t, &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), ViewOnce: proto.Bool(true)}},
				quoteTo(t, repo, quoteGroup))
		}
	})
}

// quoteStorage is real chat storage with the context of quoteDevice.
func quoteStorage(t *testing.T) (domainChatStorage.IChatStorageRepository, context.Context) {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "chatstorage.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := chatstorage.NewStorageRepository(db)
	require.NoError(t, repo.InitializeSchema())
	return repo, quoteContext(nil)
}

func quoteEvent(id string, fromMe bool, msg *waE2E.Message) *events.Message {
	sender := quoteContact
	if fromMe {
		sender = types.NewJID("550000000001", types.DefaultUserServer)
	}
	return &events.Message{
		Info:    types.MessageInfo{ID: id, Timestamp: time.Now(), MessageSource: types.MessageSource{Chat: quoteContact, Sender: sender, IsFromMe: fromMe}},
		Message: msg,
	}
}

func quoteEditEvent(id, content string) *events.Message {
	return quoteEvent("EDIT-"+id+"-"+content, true, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:           &waCommon.MessageKey{ID: proto.String(id), RemoteJID: proto.String(quoteContact.String()), FromMe: proto.Bool(true)},
		EditedMessage: &waE2E.Message{Conversation: proto.String(content)},
	}})
}

// quotedFromStorage replies to id in quoteContact's chat and returns the quote.
func quotedFromStorage(t *testing.T, repo domainChatStorage.IChatStorageRepository, ctx context.Context, id string) *waE2E.Message {
	t.Helper()
	got := serviceSend{chatStorageRepo: repo}.mergeReplyContext(ctx, nil, proto.String(id), quoteContact)
	require.Equal(t, id, got.GetStanzaID())
	return got.GetQuotedMessage()
}

// End to end on real storage: an edited photo, video, video note or document
// is quoted from its stored copy, thumbnail included, with the edited caption
// or none, and the edit leaves the stored copy as it was.
func TestQuoteOfEditedMediaKeepsItsCopy(t *testing.T) {
	repo, ctx := quoteStorage(t)
	for _, mediaType := range []string{"image", "video", "video_note", "document"} {
		for _, edited := range []string{"after", ""} {
			t.Run(mediaType+"/"+map[bool]string{true: "cleared", false: "edited"}[edited == ""], func(t *testing.T) {
				id := mediaType + "-" + edited
				require.NoError(t, repo.CreateMessage(ctx, quoteEvent(id, true, quoteWithReply(quoteFixture(mediaType)))))
				before, err := repo.GetQuotedMedia(ctx, quoteDevice, quoteContact.String(), id)
				require.NoError(t, err)

				require.NoError(t, repo.CreateMessage(ctx, quoteEditEvent(id, edited)))

				var caption *string
				if edited != "" {
					caption = proto.String(edited)
				}
				requireQuoted(t, quoteCaptioned(quoteFixture(mediaType), caption), quotedFromStorage(t, repo, ctx, id))
				after, err := repo.GetQuotedMedia(ctx, quoteDevice, quoteContact.String(), id)
				require.NoError(t, err)
				require.Equal(t, before.Copy, after.Copy)
			})
		}
	}
}

// quoteCaptioned sets the caption of the media message of m; nil removes it.
func quoteCaptioned(m *waE2E.Message, caption *string) *waE2E.Message {
	switch {
	case m.ImageMessage != nil:
		m.ImageMessage.Caption = caption
	case m.VideoMessage != nil:
		m.VideoMessage.Caption = caption
	case m.PtvMessage != nil:
		m.PtvMessage.Caption = caption
	case m.DocumentMessage != nil:
		m.DocumentMessage.Caption = caption
	}
	return m
}

// quoteWithReply gives the media message of m a reply context, as a message
// sent in reply to another one has.
func quoteWithReply(m *waE2E.Message) *waE2E.Message {
	ci := &waE2E.ContextInfo{StanzaID: proto.String("PARENT")}
	switch {
	case m.ImageMessage != nil:
		m.ImageMessage.ContextInfo = ci
	case m.VideoMessage != nil:
		m.VideoMessage.ContextInfo = ci
	case m.PtvMessage != nil:
		m.PtvMessage.ContextInfo = ci
	case m.DocumentMessage != nil:
		m.DocumentMessage.ContextInfo = ci
	}
	return m
}

// End to end: whichever way the original reaches storage around an edit (a
// late sent store, a live or history replay), the quote keeps the original
// thumbnail and shows the edited caption, or none once it was cleared.
func TestQuoteAfterAnEditAndAReplayShowsTheEditedCaption(t *testing.T) {
	repo, ctx := quoteStorage(t)
	photo := func(caption string) *waE2E.Message {
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String(caption), Mimetype: proto.String("image/jpeg"),
			JPEGThumbnail: quoteThumb, MediaKey: []byte{1}}}
	}
	for _, path := range []string{"sent before the edit", "sent after the edit", "live replay", "history replay"} {
		for _, edited := range []string{"edited", ""} {
			id := path + "/" + edited
			t.Run(id, func(t *testing.T) {
				sent := func() {
					require.NoError(t, repo.StoreSentMessageWithContext(ctx, id, quoteDevice, quoteContact.String(), "before", time.Now(), photo("before")))
				}
				edit := func() { require.NoError(t, repo.CreateMessage(ctx, quoteEditEvent(id, edited))) }
				switch path {
				case "sent before the edit":
					sent()
					edit()
				case "sent after the edit":
					edit()
					sent()
				case "live replay":
					require.NoError(t, repo.CreateMessage(ctx, quoteEvent(id, true, photo("before"))))
					edit()
					require.NoError(t, repo.CreateMessage(ctx, quoteEvent(id, true, photo("before"))))
				default:
					row := &domainChatStorage.Message{ID: id, ChatJID: quoteContact.String(), DeviceID: quoteDevice, Sender: quoteDevice,
						Content: "before", IsFromMe: true, MediaType: "image", Timestamp: time.Now(), QuotedMedia: quoteEncode(t, photo("before"))}
					require.NoError(t, repo.StoreMessagesBatch([]*domainChatStorage.Message{row}))
					edit()
					require.NoError(t, repo.StoreMessagesBatch([]*domainChatStorage.Message{row}))
				}

				want := photo("before")
				want.ImageMessage.Caption = nil
				if edited != "" {
					want.ImageMessage.Caption = proto.String(edited)
				}
				requireQuoted(t, want, quotedFromStorage(t, repo, ctx, id))
			})
		}
	}
}

// End to end: a live video note is quoted with its caption, read from the
// stored content.
func TestQuoteOfALiveVideoNoteShowsItsCaption(t *testing.T) {
	repo, ctx := quoteStorage(t)
	require.NoError(t, repo.CreateMessage(ctx, quoteEvent("ptv-1", false, quoteFixture("video_note"))))

	requireQuoted(t, quoteFixture("video_note"), quotedFromStorage(t, repo, ctx, "ptv-1"))
}
