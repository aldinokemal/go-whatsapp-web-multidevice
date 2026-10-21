package utils

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

var (
	qmJPEG  = []byte{255, 216, 255, 224}
	qmReply = &waE2E.ContextInfo{StanzaID: proto.String("PARENT"), Participant: proto.String("550000000003@s.whatsapp.net")}
)

// Fixtures of the six media messages with every field a quote may show; ci is
// their reply context, which no copy keeps.
func qmImage(ci *waE2E.ContextInfo) *waE2E.Message {
	return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption: proto.String("original"), Mimetype: proto.String("image/jpeg"), JPEGThumbnail: qmJPEG,
		Width: proto.Uint32(640), Height: proto.Uint32(480),
		URL: proto.String("https://mmg.whatsapp.net/v/image"), DirectPath: proto.String("/v/image"),
		MediaKey: []byte{1}, FileSHA256: []byte{2}, FileEncSHA256: []byte{3}, FileLength: proto.Uint64(2048),
		ContextInfo: ci,
	}}
}

func qmVideo(ci *waE2E.ContextInfo) *waE2E.VideoMessage {
	return &waE2E.VideoMessage{
		Caption: proto.String("original"), Mimetype: proto.String("video/mp4"), JPEGThumbnail: qmJPEG,
		Seconds: proto.Uint32(7), GifPlayback: proto.Bool(true),
		URL: proto.String("https://mmg.whatsapp.net/v/video"), DirectPath: proto.String("/v/video"),
		MediaKey: []byte{1}, FileSHA256: []byte{2}, FileEncSHA256: []byte{3}, FileLength: proto.Uint64(2048),
		ContextInfo: ci,
	}
}

func qmAudio(ptt bool, ci *waE2E.ContextInfo) *waE2E.Message {
	return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
		Mimetype: proto.String("audio/ogg; codecs=opus"), PTT: proto.Bool(ptt), Seconds: proto.Uint32(12), Waveform: []byte{4, 5, 6},
		URL: proto.String("https://mmg.whatsapp.net/v/audio"), DirectPath: proto.String("/v/audio"),
		MediaKey: []byte{1}, FileSHA256: []byte{2}, FileEncSHA256: []byte{3}, FileLength: proto.Uint64(2048),
		ContextInfo: ci,
	}}
}

func qmDocument(ci *waE2E.ContextInfo) *waE2E.Message {
	return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
		Caption: proto.String("original"), Mimetype: proto.String("application/pdf"), JPEGThumbnail: qmJPEG,
		Title: proto.String("Relatório"), FileName: proto.String("relatorio.pdf"),
		URL: proto.String("https://mmg.whatsapp.net/v/document"), DirectPath: proto.String("/v/document"),
		MediaKey: []byte{1}, FileSHA256: []byte{2}, FileEncSHA256: []byte{3}, FileLength: proto.Uint64(2048),
		ContextInfo: ci,
	}}
}

func qmSticker(ci *waE2E.ContextInfo) *waE2E.Message {
	return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		Mimetype: proto.String("image/webp"), IsAnimated: proto.Bool(true), PngThumbnail: []byte{137, 80, 78, 71},
		URL: proto.String("https://mmg.whatsapp.net/v/sticker"), DirectPath: proto.String("/v/sticker"),
		MediaKey: []byte{1}, FileSHA256: []byte{2}, FileEncSHA256: []byte{3}, FileLength: proto.Uint64(2048),
		ContextInfo: ci,
	}}
}

func qmEphemeral(msg *waE2E.Message) *waE2E.Message {
	return &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: msg}}
}

// requireQuotedMediaCopy checks the copy of in against want (nil: no copy), and
// that in was left as it was.
func requireQuotedMediaCopy(t *testing.T, in *waE2E.Message, viewOnce bool, want *waE2E.Message) {
	t.Helper()
	before := proto.Clone(in)
	encoded := QuotedMediaCopy(in, viewOnce)
	require.True(t, proto.Equal(before, in), "the message was modified")
	if want == nil {
		require.Nil(t, encoded)
		return
	}
	decoded := &waE2E.Message{}
	require.NoError(t, proto.Unmarshal(encoded, decoded))
	require.True(t, proto.Equal(want, decoded), "copy %v, want %v", decoded, want)
}

func TestQuotedMediaCopyKeepsTheMediaMessageWithoutItsReplyContext(t *testing.T) {
	for name, tc := range map[string]struct{ in, want *waE2E.Message }{
		"nil":            {nil, nil},
		"text":           {&waE2E.Message{Conversation: proto.String("hi")}, nil},
		"extended text":  {&waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("hi"), ContextInfo: qmReply}}, nil},
		"location":       {&waE2E.Message{LocationMessage: &waE2E.LocationMessage{DegreesLatitude: proto.Float64(1)}}, nil},
		"image":          {qmImage(qmReply), qmImage(nil)},
		"video":          {&waE2E.Message{VideoMessage: qmVideo(qmReply)}, &waE2E.Message{VideoMessage: qmVideo(nil)}},
		"video note":     {&waE2E.Message{PtvMessage: qmVideo(qmReply)}, &waE2E.Message{PtvMessage: qmVideo(nil)}},
		"voice note":     {qmAudio(true, qmReply), qmAudio(true, nil)},
		"audio":          {qmAudio(false, qmReply), qmAudio(false, nil)},
		"document":       {qmDocument(qmReply), qmDocument(nil)},
		"sticker":        {qmSticker(qmReply), qmSticker(nil)},
		"ephemeral":      {qmEphemeral(qmDocument(qmReply)), qmDocument(nil)},
		"two ephemerals": {qmEphemeral(qmEphemeral(qmSticker(qmReply))), qmSticker(nil)},
		// UnwrapMessage stops after three wrappers, and so does the copy.
		"past the wrapper limit": {qmEphemeral(qmEphemeral(qmEphemeral(qmEphemeral(qmImage(nil))))), nil},
	} {
		t.Run(name, func(t *testing.T) {
			requireQuotedMediaCopy(t, tc.in, false, tc.want)
		})
	}
}

func TestQuotedMediaCopyKeepsOnlyTheTypeOfViewOnceMedia(t *testing.T) {
	video := func(viewOnce bool) *waE2E.VideoMessage {
		v := qmVideo(qmReply)
		v.ViewOnce = proto.Bool(viewOnce)
		return v
	}
	flagged := map[string]struct{ in, want *waE2E.Message }{
		"image": {
			&waE2E.Message{ImageMessage: func() *waE2E.ImageMessage {
				m := qmImage(qmReply).ImageMessage
				m.ViewOnce = proto.Bool(true)
				return m
			}()},
			&waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), ViewOnce: proto.Bool(true)}},
		},
		"video": {
			&waE2E.Message{VideoMessage: video(true)},
			&waE2E.Message{VideoMessage: &waE2E.VideoMessage{Mimetype: proto.String("video/mp4"), ViewOnce: proto.Bool(true)}},
		},
		"video note": {
			&waE2E.Message{PtvMessage: video(true)},
			&waE2E.Message{PtvMessage: &waE2E.VideoMessage{Mimetype: proto.String("video/mp4"), ViewOnce: proto.Bool(true)}},
		},
		"voice note": {
			&waE2E.Message{AudioMessage: func() *waE2E.AudioMessage {
				m := qmAudio(true, qmReply).AudioMessage
				m.ViewOnce = proto.Bool(true)
				return m
			}()},
			&waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus"), PTT: proto.Bool(true), ViewOnce: proto.Bool(true)}},
		},
		"audio": {
			&waE2E.Message{AudioMessage: func() *waE2E.AudioMessage {
				m := qmAudio(false, qmReply).AudioMessage
				m.ViewOnce = proto.Bool(true)
				return m
			}()},
			&waE2E.Message{AudioMessage: &waE2E.AudioMessage{Mimetype: proto.String("audio/ogg; codecs=opus"), PTT: proto.Bool(false), ViewOnce: proto.Bool(true)}},
		},
	}
	unflagged := map[string]*waE2E.Message{
		"image":      qmImage(qmReply),
		"video":      {VideoMessage: video(false)},
		"video note": {PtvMessage: video(false)},
		"voice note": qmAudio(true, qmReply),
		"audio":      qmAudio(false, qmReply),
	}
	wrappers := map[string]func(*waE2E.Message) *waE2E.Message{
		"v1": func(m *waE2E.Message) *waE2E.Message {
			return &waE2E.Message{ViewOnceMessage: &waE2E.FutureProofMessage{Message: m}}
		},
		"v2": func(m *waE2E.Message) *waE2E.Message {
			return &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: m}}
		},
		"v2 extension": func(m *waE2E.Message) *waE2E.Message {
			return &waE2E.Message{ViewOnceMessageV2Extension: &waE2E.FutureProofMessage{Message: m}}
		},
		"v2 in ephemeral": func(m *waE2E.Message) *waE2E.Message {
			return qmEphemeral(&waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: m}})
		},
	}
	for name, tc := range flagged {
		t.Run(name+"/media flag", func(t *testing.T) {
			requireQuotedMediaCopy(t, tc.in, false, tc.want)
		})
		t.Run(name+"/reported by the caller", func(t *testing.T) {
			requireQuotedMediaCopy(t, unflagged[name], true, tc.want)
		})
		for wrapper, wrap := range wrappers {
			t.Run(name+"/wrapper "+wrapper, func(t *testing.T) {
				requireQuotedMediaCopy(t, wrap(unflagged[name]), false, tc.want)
			})
		}
	}

	// WhatsApp has no view-once documents or stickers: rather than a full copy
	// of something marked view-once, there is none.
	for name, msg := range map[string]*waE2E.Message{"document": qmDocument(qmReply), "sticker": qmSticker(qmReply)} {
		t.Run(name+"/reported by the caller", func(t *testing.T) {
			requireQuotedMediaCopy(t, msg, true, nil)
		})
		for wrapper, wrap := range wrappers {
			t.Run(name+"/wrapper "+wrapper, func(t *testing.T) {
				requireQuotedMediaCopy(t, wrap(msg), false, nil)
			})
		}
	}
}

func TestQuotedMediaCopyStaysUnder64KiB(t *testing.T) {
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
	require.Len(t, QuotedMediaCopy(sized(65535), false), 65535)
	require.Nil(t, QuotedMediaCopy(sized(65536), false))
}

// The caption of a video note is its content, as for a video.
func TestExtractMessageTextFromProtoReadsVideoNoteCaptions(t *testing.T) {
	note := &waE2E.Message{PtvMessage: &waE2E.VideoMessage{Caption: proto.String("ptv"), JPEGThumbnail: qmJPEG}}
	require.Equal(t, "ptv", ExtractMessageTextFromProto(note))
	require.Equal(t, "ptv", ExtractMessageTextFromProto(qmEphemeral(note)))
	require.Equal(t, "", ExtractMessageTextFromProto(&waE2E.Message{PtvMessage: &waE2E.VideoMessage{JPEGThumbnail: qmJPEG}}))
}
