package whatsapp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func businessEvent(id string, msg *waE2E.Message) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("628123456789", types.DefaultUserServer),
				Sender: types.NewJID("628123456789", types.DefaultUserServer),
			},
			ID:        id,
			Timestamp: time.Date(2026, time.September, 25, 10, 0, 0, 0, time.UTC),
		},
		Message: msg,
	}
}

func businessOrderTemplate() *waE2E.TemplateMessage {
	return &waE2E.TemplateMessage{
		TemplateID: proto.String("order_confirmed"),
		HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
			Title:               &waE2E.TemplateMessage_HydratedFourRowTemplate_HydratedTitleText{HydratedTitleText: "Order confirmed"},
			HydratedContentText: proto.String("Hi John, your order #1234 has shipped."),
			HydratedFooterText:  proto.String("Acme Store"),
			HydratedButtons: []*waE2E.HydratedTemplateButton{
				{HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{
					DisplayText: proto.String("Track order"), URL: proto.String("https://acme.example/track/1234")}}},
			},
		},
	}
}

func TestBuildEventPayloadTemplateCarriesStructuredFieldsAndBody(t *testing.T) {
	_, payload, err := buildEventPayload(context.Background(), nil, businessEvent("TPL1", &waE2E.Message{TemplateMessage: businessOrderTemplate()}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	template, ok := payload["template"].(utils.BusinessMessage)
	if !ok {
		t.Fatalf("expected template payload, got %T", payload["template"])
	}
	if template.Title != "Order confirmed" || template.TemplateID != "order_confirmed" || len(template.Buttons) != 1 {
		t.Fatalf("template not kept: %+v", template)
	}
	want := "Order confirmed\nHi John, your order #1234 has shipped.\nAcme Store\n🔗 Track order: https://acme.example/track/1234"
	if payload["body"] != want {
		t.Fatalf("body = %q, want %q", payload["body"], want)
	}
}

func TestBuildEventPayloadListReplyWithoutTitle(t *testing.T) {
	reply := &waE2E.ListResponseMessage{
		SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String("row-void-pnr")},
	}
	_, payload, err := buildEventPayload(context.Background(), nil, businessEvent("LR1", &waE2E.Message{ListResponseMessage: reply}))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	selection, ok := payload["selection"].(utils.BusinessSelection)
	if !ok || selection.Kind != "list" || selection.SelectedID != "row-void-pnr" {
		t.Fatalf("selected row not kept: %#v", payload["selection"])
	}
	if payload["body"] != "Selected option row-void-pnr" {
		t.Fatalf("unexpected body %q", payload["body"])
	}
}

func TestBusinessMessagesAreRecognized(t *testing.T) {
	for name, msg := range map[string]*waE2E.Message{
		"interactive":    {InteractiveMessage: &waE2E.InteractiveMessage{}},
		"template":       {TemplateMessage: businessOrderTemplate()},
		"buttons":        {ButtonsMessage: &waE2E.ButtonsMessage{}},
		"product":        {ProductMessage: &waE2E.ProductMessage{}},
		"list reply":     {ListResponseMessage: &waE2E.ListResponseMessage{}},
		"buttons reply":  {ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{}},
		"template reply": {TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{}},
		"native reply":   {InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{}},
	} {
		if !hasRecognizedMessageType(msg) {
			t.Errorf("%s should be a recognized message type", name)
		}
	}
}

// Runs each message the way Chatwoot receives it, live and after the retry
// queue's JSON round-trip.
func TestChatwootContentForBusinessMessages(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "template",
			msg:  &waE2E.Message{TemplateMessage: businessOrderTemplate()},
			want: "Order confirmed\nHi John, your order #1234 has shipped.\nAcme Store\n🔗 Track order: https://acme.example/track/1234",
		},
		{
			// The interactive summary skips the markdown pass, so the
			// underscores in the URL stay intact.
			name: "interactive with header, footer and cta_url",
			msg: &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
				Header: &waE2E.InteractiveMessage_Header{Title: proto.String("Support")},
				Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("How can we help?")},
				Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String("Acme Support")},
				InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
					Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
						Name:             proto.String("cta_url"),
						ButtonParamsJSON: proto.String(`{"display_text":"Visit site","url":"https://acme.example/help?utm_source=wa&utm_medium=cta"}`),
					}},
				}},
			}},
			want: "Support\nHow can we help?\nAcme Support\n🔗 Visit site: https://acme.example/help?utm_source=wa&utm_medium=cta",
		},
		{
			name: "product",
			msg: &waE2E.Message{ProductMessage: &waE2E.ProductMessage{Product: &waE2E.ProductMessage_ProductSnapshot{
				Title: proto.String("Weekend package"), CurrencyCode: proto.String("IDR"), PriceAmount1000: proto.Int64(3500000),
			}}},
			want: "Product: Weekend package (IDR 3500.00)",
		},
		{
			name: "list reply with only a row id",
			msg: &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{
				SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String("opt_book_now")},
			}},
			want: "Selected option opt_book_now",
		},
		{
			// Template text goes through the markdown pass: *bold* converts,
			// but the URL's underscores must not turn into italics.
			name: "template with formatting and a UTM link",
			msg: &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
				HydratedContentText: proto.String("Your order *#1234* has shipped."),
				HydratedButtons: []*waE2E.HydratedTemplateButton{
					{HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{
						DisplayText: proto.String("Track"), URL: proto.String("https://acme.example/t?utm_source=wa&utm_medium=tpl")}}},
				},
			}}},
			want: "Your order **#1234** has shipped.\n🔗 Track: https://acme.example/t?utm_source=wa&utm_medium=tpl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, payload, err := buildEventPayload(context.Background(), nil, businessEvent("CW1", tt.msg))
			if err != nil {
				t.Fatalf("buildEventPayload: %v", err)
			}
			if live, _ := buildChatwootMessageContent(payload, false, ""); live != tt.want {
				t.Fatalf("live content = %q, want %q", live, tt.want)
			}

			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var replayed map[string]any
			if err := json.Unmarshal(raw, &replayed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if retried, _ := buildChatwootMessageContent(replayed, false, ""); retried != tt.want {
				t.Fatalf("retried content = %q, want %q", retried, tt.want)
			}
		})
	}
}
