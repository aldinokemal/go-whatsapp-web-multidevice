package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

func TestBuildEventPayloadAddsMetadataFields(t *testing.T) {
	oldAutoDownload := config.WhatsappAutoDownloadMedia
	config.WhatsappAutoDownloadMedia = false
	defer func() { config.WhatsappAutoDownloadMedia = oldAutoDownload }()

	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("5541988887777", types.DefaultUserServer),
				Sender: types.NewJID("5541988887777", types.DefaultUserServer),
			},
			ID:        "MSG900",
			Timestamp: time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				URL:        proto.String("https://mmg.whatsapp.net/image"),
				Caption:    proto.String("look"),
				Mimetype:   proto.String("image/jpeg"),
				FileLength: proto.Uint64(2048),
				Width:      proto.Uint32(800),
				Height:     proto.Uint32(600),
				ContextInfo: &waE2E.ContextInfo{
					ForwardingScore:  proto.Uint32(6),
					MentionedJID:     []string{"5541977776666@s.whatsapp.net"},
					Expiration:       proto.Uint32(86400),
					ConversionSource: proto.String("FB_Ads"),
				},
			},
		},
	}

	eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
	require.NoError(t, err)
	assert.Equal(t, EventTypeMessage, eventType)

	assert.Equal(t, uint32(6), payload["forwarding_score"])
	assert.Equal(t, []string{"5541977776666@s.whatsapp.net"}, payload["mentioned_jids"])
	assert.Equal(t, uint32(86400), payload["expiration"])
	assert.Equal(t, map[string]any{"conversion_source": "FB_Ads"}, payload["conversion"])
	assert.Equal(t, map[string]any{
		"mime_type":   "image/jpeg",
		"file_length": uint64(2048),
		"width":       uint32(800),
		"height":      uint32(600),
	}, payload["media_info"])

	assert.Equal(t, map[string]any{
		"url":     "https://mmg.whatsapp.net/image",
		"caption": "look",
	}, payload["image"])
	assert.Equal(t, "look", payload["body"])
}

func TestBuildEventPayloadOmitsAbsentMetadataFields(t *testing.T) {
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("5541988887777", types.DefaultUserServer),
				Sender: types.NewJID("5541988887777", types.DefaultUserServer),
			},
			ID:        "MSG901",
			Timestamp: time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("reply"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:        proto.String("QUOTED1"),
					ForwardingScore: proto.Uint32(0),
					MentionedJID:    []string{},
					QuotedMessage: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
						Text: proto.String("ad"),
						ContextInfo: &waE2E.ContextInfo{
							ConversionSource: proto.String("FB_Ads"),
							ForwardingScore:  proto.Uint32(3),
							Expiration:       proto.Uint32(86400),
						},
					}},
				},
			},
		},
	}

	_, payload, err := buildEventPayload(context.Background(), nil, evt)
	require.NoError(t, err)

	for _, key := range []string{"conversion", "media_info", "forwarding_score", "mentioned_jids", "expiration"} {
		assert.NotContains(t, payload, key)
	}
}

func TestAddMessageMetadataFieldsNormalizesMentionedLIDs(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	pn := types.NewJID("5541977776666", types.DefaultUserServer)
	client := &whatsmeow.Client{Store: &store.Device{
		LIDs: &chatDisplayNameLIDStore{lid: lid, pn: pn},
	}}
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("@123456789012345 @999 @5541955554444"),
		ContextInfo: &waE2E.ContextInfo{MentionedJID: []string{
			lid.String(),
			"999@lid",
			"5541955554444@s.whatsapp.net",
		}},
	}}

	t.Run("resolves known LIDs", func(t *testing.T) {
		payload := map[string]any{}
		addMessageMetadataFields(context.Background(), client, msg, payload)
		assert.Equal(t, []string{pn.String(), "999@lid", "5541955554444@s.whatsapp.net"}, payload["mentioned_jids"])
	})

	t.Run("keeps JIDs without a client", func(t *testing.T) {
		counter := &warnCountingLogger{Logger: waLog.Noop}
		log = counter
		defer func() { log = waLog.Noop }()

		payload := map[string]any{}
		addMessageMetadataFields(context.Background(), nil, msg, payload)
		assert.Equal(t, []string{lid.String(), "999@lid", "5541955554444@s.whatsapp.net"}, payload["mentioned_jids"])
		assert.Zero(t, counter.warns)
	})
}

func TestAddMessageMetadataFieldsUnwrapsMessage(t *testing.T) {
	msg := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("disappearing"),
			ContextInfo: &waE2E.ContextInfo{
				Expiration:      proto.Uint32(604800),
				ForwardingScore: proto.Uint32(2),
			},
		},
	}}}

	payload := map[string]any{}
	addMessageMetadataFields(context.Background(), nil, msg, payload)

	assert.Equal(t, uint32(604800), payload["expiration"])
	assert.Equal(t, uint32(2), payload["forwarding_score"])
}

type warnCountingLogger struct {
	waLog.Logger
	warns int
}

func (l *warnCountingLogger) Warnf(string, ...any) {
	l.warns++
}
