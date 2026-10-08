package utils

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestExtractContextMetadata(t *testing.T) {
	// Escapes must survive two JSON layers: metadata itself and the history API.
	parent := "parent-\"quoted\"\\id\n"
	ci := &waE2E.ContextInfo{StanzaID: proto.String(parent)}
	messages := map[string]*waE2E.Message{
		"text":          {ExtendedTextMessage: &waE2E.ExtendedTextMessage{ContextInfo: ci}},
		"image":         {ImageMessage: &waE2E.ImageMessage{ContextInfo: ci}},
		"video":         {VideoMessage: &waE2E.VideoMessage{ContextInfo: ci}},
		"audio":         {AudioMessage: &waE2E.AudioMessage{ContextInfo: ci}},
		"document":      {DocumentMessage: &waE2E.DocumentMessage{ContextInfo: ci}},
		"sticker":       {StickerMessage: &waE2E.StickerMessage{ContextInfo: ci}},
		"contact":       {ContactMessage: &waE2E.ContactMessage{ContextInfo: ci}},
		"location":      {LocationMessage: &waE2E.LocationMessage{ContextInfo: ci}},
		"video note":    {PtvMessage: &waE2E.VideoMessage{ContextInfo: ci}},
		"live location": {LiveLocationMessage: &waE2E.LiveLocationMessage{ContextInfo: ci}},
		"interactive":   {InteractiveMessage: &waE2E.InteractiveMessage{ContextInfo: ci}},
	}
	for name, msg := range messages {
		t.Run(name, func(t *testing.T) {
			original := proto.Clone(msg)
			var metadata map[string]string
			require.NoError(t, json.Unmarshal([]byte(ExtractContextMetadata(msg)), &metadata))
			require.Equal(t, map[string]string{"replied_to_id": parent}, metadata)
			require.True(t, proto.Equal(original, msg), "extraction must not mutate the message")
		})
	}
	for name, wrap := range map[string]func(*waE2E.Message) *waE2E.Message{
		"ephemeral": func(msg *waE2E.Message) *waE2E.Message {
			return &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: msg}}
		},
		"view once": func(msg *waE2E.Message) *waE2E.Message {
			return &waE2E.Message{ViewOnceMessage: &waE2E.FutureProofMessage{Message: msg}}
		},
		"view once v2": func(msg *waE2E.Message) *waE2E.Message {
			return &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: msg}}
		},
		"view once v2 extension": func(msg *waE2E.Message) *waE2E.Message {
			return &waE2E.Message{ViewOnceMessageV2Extension: &waE2E.FutureProofMessage{Message: msg}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			msg := messages["image"]
			require.Equal(t, ExtractContextMetadata(msg), ExtractContextMetadata(wrap(msg)))
		})
	}
	for _, msg := range []*waE2E.Message{
		nil, {}, {Conversation: proto.String("ordinary")},
		{ExtendedTextMessage: &waE2E.ExtendedTextMessage{ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{"mention@s.whatsapp.net"}, IsForwarded: proto.Bool(true)}}},
		{EphemeralMessage: &waE2E.FutureProofMessage{}},
	} {
		require.Empty(t, ExtractContextMetadata(msg))
	}
}
