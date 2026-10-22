package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestExtractConversion(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want map[string]any
	}{
		{
			name: "entry point attribution",
			msg: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("hi"),
				ContextInfo: &waE2E.ContextInfo{
					ConversionSource:                 proto.String("FB_Ads"),
					ConversionData:                   []byte{1, 2},
					EntryPointConversionSource:       proto.String("ctwa_ad"),
					EntryPointConversionApp:          proto.String("instagram"),
					EntryPointConversionDelaySeconds: proto.Uint32(4),
				},
			}},
			want: map[string]any{
				"conversion_source":                    "FB_Ads",
				"conversion_data":                      "AQI=",
				"entry_point_conversion_source":        "ctwa_ad",
				"entry_point_conversion_app":           "instagram",
				"entry_point_conversion_delay_seconds": uint32(4),
			},
		},
		{
			name: "delay and external entry point",
			msg: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
				ContextInfo: &waE2E.ContextInfo{
					ConversionDelaySeconds:             proto.Uint32(30),
					EntryPointConversionExternalSource: proto.String("google"),
					EntryPointConversionExternalMedium: proto.String("cpc"),
				},
			}},
			want: map[string]any{
				"conversion_delay_seconds":               uint32(30),
				"entry_point_conversion_external_source": "google",
				"entry_point_conversion_external_medium": "cpc",
			},
		},
		{
			name: "wrapped in ephemeral message",
			msg: &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text:        proto.String("hi"),
					ContextInfo: &waE2E.ContextInfo{ConversionSource: proto.String("FB_Ads")},
				},
			}}},
			want: map[string]any{"conversion_source": "FB_Ads"},
		},
		{
			name: "empty conversion data is omitted",
			msg: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				ContextInfo: &waE2E.ContextInfo{
					ConversionSource: proto.String("FB_Ads"),
					ConversionData:   []byte{},
				},
			}},
			want: map[string]any{"conversion_source": "FB_Ads"},
		},
		{
			name: "only empty conversion data",
			msg: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				ContextInfo: &waE2E.ContextInfo{ConversionData: []byte{}},
			}},
			want: nil,
		},
		{
			name: "attribution on the quoted message is ignored",
			msg: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("replying to the ad"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID: proto.String("QUOTED1"),
					QuotedMessage: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
						Text:        proto.String("original"),
						ContextInfo: &waE2E.ContextInfo{ConversionSource: proto.String("FB_Ads")},
					}},
				},
			}},
			want: nil,
		},
		{
			name: "no attribution fields",
			msg: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String("hi"),
				ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("X")},
			}},
			want: nil,
		},
		{
			name: "plain conversation",
			msg:  &waE2E.Message{Conversation: proto.String("hi")},
			want: nil,
		},
		{
			name: "nil message",
			msg:  nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExtractConversion(tt.msg))
		})
	}
}

func TestExtractMediaMetadata(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want map[string]any
	}{
		{
			name: "voice note",
			msg: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
				Mimetype:   proto.String("audio/ogg; codecs=opus"),
				Seconds:    proto.Uint32(7),
				PTT:        proto.Bool(true),
				FileLength: proto.Uint64(1234),
			}},
			want: map[string]any{
				"mime_type":   "audio/ogg; codecs=opus",
				"seconds":     uint32(7),
				"ptt":         true,
				"file_length": uint64(1234),
			},
		},
		{
			name: "audio file without ptt",
			msg: &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
				Mimetype: proto.String("audio/mpeg"),
				PTT:      proto.Bool(false),
			}},
			want: map[string]any{"mime_type": "audio/mpeg"},
		},
		{
			name: "document",
			msg: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
				FileName: proto.String("a.pdf"),
			}},
			want: map[string]any{"file_name": "a.pdf"},
		},
		{
			name: "image",
			msg: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
				Mimetype:   proto.String("image/jpeg"),
				FileLength: proto.Uint64(2048),
				FileSHA256: []byte{1, 2},
				Width:      proto.Uint32(800),
				Height:     proto.Uint32(600),
			}},
			want: map[string]any{
				"mime_type":   "image/jpeg",
				"file_length": uint64(2048),
				"file_sha256": "AQI=",
				"width":       uint32(800),
				"height":      uint32(600),
			},
		},
		{
			name: "gif video",
			msg: &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
				Mimetype:    proto.String("video/mp4"),
				Seconds:     proto.Uint32(3),
				Width:       proto.Uint32(320),
				Height:      proto.Uint32(240),
				GifPlayback: proto.Bool(true),
			}},
			want: map[string]any{
				"mime_type":    "video/mp4",
				"seconds":      uint32(3),
				"width":        uint32(320),
				"height":       uint32(240),
				"gif_playback": true,
			},
		},
		{
			name: "video note",
			msg: &waE2E.Message{PtvMessage: &waE2E.VideoMessage{
				Mimetype: proto.String("video/mp4"),
				Seconds:  proto.Uint32(12),
				Width:    proto.Uint32(480),
				Height:   proto.Uint32(480),
			}},
			want: map[string]any{
				"mime_type": "video/mp4",
				"seconds":   uint32(12),
				"width":     uint32(480),
				"height":    uint32(480),
			},
		},
		{
			name: "animated sticker",
			msg: &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
				Mimetype:   proto.String("image/webp"),
				Width:      proto.Uint32(512),
				Height:     proto.Uint32(512),
				IsAnimated: proto.Bool(true),
			}},
			want: map[string]any{
				"mime_type":   "image/webp",
				"width":       uint32(512),
				"height":      uint32(512),
				"is_animated": true,
			},
		},
		{
			name: "view once image",
			msg: &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: &waE2E.Message{
				ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg")},
			}}},
			want: map[string]any{"mime_type": "image/jpeg"},
		},
		{
			name: "empty sha256 is omitted",
			msg: &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
				Mimetype:   proto.String("application/pdf"),
				FileSHA256: []byte{},
			}},
			want: map[string]any{"mime_type": "application/pdf"},
		},
		{
			name: "media without metadata",
			msg:  &waE2E.Message{ImageMessage: &waE2E.ImageMessage{}},
			want: nil,
		},
		{
			name: "plain text",
			msg:  &waE2E.Message{Conversation: proto.String("hi")},
			want: nil,
		},
		{
			name: "nil message",
			msg:  nil,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExtractMediaMetadata(tt.msg))
		})
	}
}
