package whatsapp

import (
	"context"
	"encoding/json"
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

func interactiveTestEvent(id string, msg *waE2E.Message) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("5541988887777", types.DefaultUserServer),
				Sender: types.NewJID("5541988887777", types.DefaultUserServer),
			},
			ID:        id,
			Timestamp: time.Date(2026, time.October, 22, 10, 0, 0, 0, time.UTC),
		},
		Message: msg,
	}
}

func TestBuildEventPayloadAddsInteractiveReply(t *testing.T) {
	tests := []struct {
		name      string
		msg       *waE2E.Message
		wantReply map[string]any
		wantBody  string
	}{
		{
			name: "button",
			msg: &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
				SelectedButtonID: proto.String("b1"),
				Response:         &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Sim"},
			}},
			wantReply: map[string]any{"kind": "button", "id": "b1", "title": "Sim"},
			wantBody:  "Sim",
		},
		{
			name: "list",
			msg: &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{
				Title:             proto.String("Plano"),
				SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String("r2")},
			}},
			wantReply: map[string]any{"kind": "list", "id": "r2", "title": "Plano"},
			wantBody:  "Plano",
		},
		{
			name: "template button",
			msg: &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
				SelectedID:          proto.String("t1"),
				SelectedDisplayText: proto.String("Ok"),
			}},
			wantReply: map[string]any{"kind": "template_button", "id": "t1", "title": "Ok"},
			wantBody:  "Ok",
		},
		{
			name: "native flow",
			msg: &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
				InteractiveResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage_{
					NativeFlowResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage{
						Name:       proto.String("flow"),
						ParamsJSON: proto.String("{}"),
					},
				},
				Body: &waE2E.InteractiveResponseMessage_Body{Text: proto.String("Enviado")},
			}},
			wantReply: map[string]any{"kind": "native_flow", "name": "flow", "response_json": "{}", "title": "Enviado"},
			wantBody:  "Enviado",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventType, payload, err := buildEventPayload(context.Background(), nil, interactiveTestEvent("MSG910", tt.msg))
			require.NoError(t, err)
			assert.Equal(t, EventTypeMessage, eventType)
			assert.Equal(t, tt.wantReply, payload["interactive_reply"])
			assert.Equal(t, tt.wantBody, payload["body"])
		})
	}
}

func TestBuildEventPayloadAddsOrderFields(t *testing.T) {
	order := &waE2E.OrderMessage{
		OrderID:           proto.String("o1"),
		OrderTitle:        proto.String("Loja"),
		Message:           proto.String("Quero estes"),
		ItemCount:         proto.Int32(2),
		TotalAmount1000:   proto.Int64(150000),
		TotalCurrencyCode: proto.String("BRL"),
		Thumbnail:         []byte{1, 2},
		Status:            waE2E.OrderMessage_INQUIRY.Enum(),
	}

	_, payload, err := buildEventPayload(context.Background(), nil, interactiveTestEvent("MSG911", &waE2E.Message{OrderMessage: order}))
	require.NoError(t, err)
	assert.Equal(t, "Order: Loja\nQuero estes", payload["body"])

	titled, ok := payload["order"].(interface{ GetOrderTitle() string })
	require.True(t, ok)
	assert.Equal(t, "Loja", titled.GetOrderTitle())

	upstreamJSON, err := json.Marshal(order)
	require.NoError(t, err)
	var upstream map[string]any
	require.NoError(t, json.Unmarshal(upstreamJSON, &upstream))

	gotJSON, err := json.Marshal(payload["order"])
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(gotJSON, &got))

	assert.Contains(t, upstream, "orderTitle")
	for key, value := range upstream {
		assert.Equal(t, value, got[key], key)
	}
	assert.Equal(t, "o1", got["order_id"])
	assert.Equal(t, "Loja", got["title"])
	assert.Equal(t, "Quero estes", got["message"])
	assert.Equal(t, float64(2), got["item_count"])
	assert.Equal(t, float64(150000), got["total_amount_1000"])
	assert.Equal(t, "BRL", got["currency"])
	assert.Len(t, got, len(upstream)+5)
}

func TestBuildEventPayloadKeepsOrderBody(t *testing.T) {
	tests := []struct {
		name     string
		order    *waE2E.OrderMessage
		wantBody string
	}{
		{name: "title only", order: &waE2E.OrderMessage{OrderTitle: proto.String("Loja")}, wantBody: "Order: Loja"},
		{name: "message only", order: &waE2E.OrderMessage{Message: proto.String("Quero estes")}, wantBody: "Quero estes"},
		{name: "title and message", order: &waE2E.OrderMessage{OrderTitle: proto.String("Loja"), Message: proto.String("Quero estes")}, wantBody: "Order: Loja\nQuero estes"},
		{name: "neither", order: &waE2E.OrderMessage{OrderID: proto.String("o1")}, wantBody: "Order message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, payload, err := buildEventPayload(context.Background(), nil, interactiveTestEvent("MSG912", &waE2E.Message{OrderMessage: tt.order}))
			require.NoError(t, err)
			assert.Equal(t, tt.wantBody, payload["body"])
			assert.Contains(t, payload, "order")
		})
	}
}

func TestAddMessageMetadataFieldsKeepsExistingBody(t *testing.T) {
	msg := &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
		Response: &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Sim"},
	}}

	payload := map[string]any{"body": "typed"}
	addMessageMetadataFields(context.Background(), nil, msg, payload)

	assert.Equal(t, "typed", payload["body"])
	assert.Equal(t, map[string]any{"kind": "button", "title": "Sim"}, payload["interactive_reply"])
}

func TestInteractiveRepliesAreRecognized(t *testing.T) {
	for _, msg := range []*waE2E.Message{
		{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{}},
		{ListResponseMessage: &waE2E.ListResponseMessage{}},
		{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{}},
		{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
			InteractiveResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage_{
				NativeFlowResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage{},
			},
		}},
	} {
		assert.True(t, hasRecognizedMessageType(msg), populatedMessageFields(msg))
	}

	assert.False(t, payloadHasNoRenderableContent(map[string]any{"interactive_reply": map[string]any{"kind": "button"}}))
}
