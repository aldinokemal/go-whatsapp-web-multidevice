package whatsapp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

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

func hydratedOrderTemplate() *waE2E.TemplateMessage {
	return &waE2E.TemplateMessage{
		TemplateID: proto.String("order_confirmed"),
		HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
			Title:               &waE2E.TemplateMessage_HydratedFourRowTemplate_HydratedTitleText{HydratedTitleText: "Order confirmed"},
			HydratedContentText: proto.String("Hi John, your order #1234 has shipped."),
			HydratedFooterText:  proto.String("Acme Store"),
			HydratedButtons: []*waE2E.HydratedTemplateButton{
				{HydratedButton: &waE2E.HydratedTemplateButton_UrlButton{UrlButton: &waE2E.HydratedTemplateButton_HydratedURLButton{
					DisplayText: proto.String("Track order"), URL: proto.String("https://acme.example/track/1234")}}},
				{HydratedButton: &waE2E.HydratedTemplateButton_CallButton{CallButton: &waE2E.HydratedTemplateButton_HydratedCallButton{
					DisplayText: proto.String("Call us"), PhoneNumber: proto.String("+15550100")}}},
				{HydratedButton: &waE2E.HydratedTemplateButton_QuickReplyButton{QuickReplyButton: &waE2E.HydratedTemplateButton_HydratedQuickReplyButton{
					DisplayText: proto.String("Stop updates"), ID: proto.String("stop")}}},
			},
		},
	}
}

func TestBuildEventPayloadTemplateKeepsAllFourRows(t *testing.T) {
	evt := businessEvent("TPL1", &waE2E.Message{TemplateMessage: hydratedOrderTemplate()})

	_, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	template, ok := payload["template"].(webhookBusinessMessage)
	if !ok {
		t.Fatalf("expected template payload, got %T", payload["template"])
	}
	if template.Title != "Order confirmed" || template.Body != "Hi John, your order #1234 has shipped." || template.Footer != "Acme Store" {
		t.Fatalf("title/body/footer not kept: %+v", template)
	}
	if template.TemplateID != "order_confirmed" {
		t.Fatalf("expected template id, got %q", template.TemplateID)
	}
	want := []webhookBusinessButton{
		{Type: "url", Text: "Track order", URL: "https://acme.example/track/1234"},
		{Type: "call", Text: "Call us", Phone: "+15550100"},
		{Type: "quick_reply", Text: "Stop updates", ID: "stop"},
	}
	if len(template.Buttons) != len(want) {
		t.Fatalf("expected %d buttons, got %+v", len(want), template.Buttons)
	}
	for i := range want {
		if !reflect.DeepEqual(template.Buttons[i], want[i]) {
			t.Fatalf("button %d: expected %+v, got %+v", i, want[i], template.Buttons[i])
		}
	}
	if _, hasBody := payload["body"]; hasBody {
		t.Fatalf("body must stay unset so body-keyed consumers keep their behaviour, got %q", payload["body"])
	}
}

func TestBuildEventPayloadTemplateFromFourRowOneof(t *testing.T) {
	tm := &waE2E.TemplateMessage{Format: &waE2E.TemplateMessage_HydratedFourRowTemplate_{
		HydratedFourRowTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
			HydratedContentText: proto.String("Your code is 482913"),
		},
	}}
	_, payload, _ := buildEventPayload(context.Background(), nil, businessEvent("TPL2", &waE2E.Message{TemplateMessage: tm}))
	template := payload["template"].(webhookBusinessMessage)
	if template.Body != "Your code is 482913" {
		t.Fatalf("expected body from the hydratedFourRowTemplate oneof, got %+v", template)
	}
}

func TestBuildEventPayloadTemplateWrappingInteractiveMessage(t *testing.T) {
	tm := &waE2E.TemplateMessage{Format: &waE2E.TemplateMessage_InteractiveMessageTemplate{
		InteractiveMessageTemplate: &waE2E.InteractiveMessage{
			Header: &waE2E.InteractiveMessage_Header{Title: proto.String("Promo")},
			Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("20% off today")},
			InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
				Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
					Name:             proto.String("cta_copy"),
					ButtonParamsJSON: proto.String(`{"display_text":"Copy code","copy_code":"SAVE20"}`),
				}},
			}},
		},
	}}
	_, payload, _ := buildEventPayload(context.Background(), nil, businessEvent("TPL3", &waE2E.Message{TemplateMessage: tm}))
	template := payload["template"].(webhookBusinessMessage)
	if template.Title != "Promo" || template.Body != "20% off today" {
		t.Fatalf("interactive template rows not kept: %+v", template)
	}
	if len(template.Buttons) != 1 || !reflect.DeepEqual(template.Buttons[0], webhookBusinessButton{Type: "copy", Text: "Copy code", Code: "SAVE20"}) {
		t.Fatalf("expected copy-code button, got %+v", template.Buttons)
	}
}

func TestBuildEventPayloadButtonsMessage(t *testing.T) {
	bm := &waE2E.ButtonsMessage{
		Header:      &waE2E.ButtonsMessage_Text{Text: "Travel desk"},
		ContentText: proto.String("Please choose the service"),
		FooterText:  proto.String("Reply with a button"),
		Buttons: []*waE2E.ButtonsMessage_Button{
			{ButtonID: proto.String("sales"), ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String("Sales")},
				Type: waE2E.ButtonsMessage_Button_RESPONSE.Enum()},
			{ButtonID: proto.String("support"), ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String("Support")},
				Type: waE2E.ButtonsMessage_Button_RESPONSE.Enum()},
		},
	}
	_, payload, _ := buildEventPayload(context.Background(), nil, businessEvent("BTN1", &waE2E.Message{ButtonsMessage: bm}))
	buttons, ok := payload["buttons"].(webhookBusinessMessage)
	if !ok {
		t.Fatalf("expected buttons payload, got %T", payload["buttons"])
	}
	if buttons.Title != "Travel desk" || buttons.Body != "Please choose the service" || buttons.Footer != "Reply with a button" {
		t.Fatalf("rows not kept: %+v", buttons)
	}
	if len(buttons.Buttons) != 2 || !reflect.DeepEqual(buttons.Buttons[1], webhookBusinessButton{Type: "quick_reply", Text: "Support", ID: "support"}) {
		t.Fatalf("buttons not kept: %+v", buttons.Buttons)
	}
}

func TestBuildEventPayloadProductMessage(t *testing.T) {
	pm := &waE2E.ProductMessage{
		Product: &waE2E.ProductMessage_ProductSnapshot{
			ProductID: proto.String("p-77"), Title: proto.String("Weekend package"),
			CurrencyCode: proto.String("IDR"), PriceAmount1000: proto.Int64(3500000),
		},
		BusinessOwnerJID: proto.String("628111222333@s.whatsapp.net"),
		Catalog:          &waE2E.ProductMessage_CatalogSnapshot{Title: proto.String("Packages")},
	}
	_, payload, _ := buildEventPayload(context.Background(), nil, businessEvent("PRD1", &waE2E.Message{ProductMessage: pm}))
	product, ok := payload["product"].(webhookProductPayload)
	if !ok {
		t.Fatalf("expected product payload, got %T", payload["product"])
	}
	if product.Title != "Weekend package" || product.PriceAmount1000 != 3500000 || product.CatalogTitle != "Packages" {
		t.Fatalf("product not kept: %+v", product)
	}
	if got := formatProductSummary(product); got != "Product: Weekend package (IDR 3500.00)" {
		t.Fatalf("unexpected product summary %q", got)
	}
}

// Recent clients send list replies with only the row id.
func TestBuildEventPayloadListReplyWithoutTitleKeepsSelectedRow(t *testing.T) {
	reply := &waE2E.ListResponseMessage{
		ListType:          waE2E.ListResponseMessage_SINGLE_SELECT.Enum(),
		SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String("row-void-pnr")},
	}
	_, payload, _ := buildEventPayload(context.Background(), nil, businessEvent("LR1", &waE2E.Message{ListResponseMessage: reply}))
	selection, ok := payload["selection"].(webhookSelectionPayload)
	if !ok {
		t.Fatalf("expected selection payload, got %T", payload["selection"])
	}
	if selection.Kind != "list" || selection.SelectedID != "row-void-pnr" {
		t.Fatalf("selected row not kept: %+v", selection)
	}
	if payloadHasNoRenderableContent(payload) {
		t.Fatal("a list reply must count as renderable content")
	}
}

func TestBusinessMessagesAreRecognized(t *testing.T) {
	for name, msg := range map[string]*waE2E.Message{
		"template":       {TemplateMessage: hydratedOrderTemplate()},
		"buttons":        {ButtonsMessage: &waE2E.ButtonsMessage{ContentText: proto.String("x")}},
		"product":        {ProductMessage: &waE2E.ProductMessage{}},
		"list reply":     {ListResponseMessage: &waE2E.ListResponseMessage{}},
		"buttons reply":  {ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{}},
		"template reply": {TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{}},
	} {
		if !hasRecognizedMessageType(msg) {
			t.Errorf("%s should be a recognized message type", name)
		}
	}
}

func TestChatwootContentForTemplateSurvivesRetryRoundTrip(t *testing.T) {
	_, payload, _ := buildEventPayload(context.Background(), nil, businessEvent("TPL4", &waE2E.Message{TemplateMessage: hydratedOrderTemplate()}))
	live := extractStructuredMessageContent(payload)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var replayed map[string]any
	if err := json.Unmarshal(raw, &replayed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	retried := extractStructuredMessageContent(replayed)

	if live != retried {
		t.Fatalf("live and retried content differ:\n%s\n---\n%s", live, retried)
	}
	for _, want := range []string{"Order confirmed", "Hi John, your order #1234 has shipped.", "Acme Store",
		"🔗 Track order: https://acme.example/track/1234", "📞 Call us: +15550100", "[Stop updates]"} {
		if !strings.Contains(live, want) {
			t.Fatalf("expected %q in Chatwoot content:\n%s", want, live)
		}
	}
}

func TestChatwootInteractiveContentUnchanged(t *testing.T) {
	im := &waE2E.InteractiveMessage{Body: &waE2E.InteractiveMessage_Body{Text: proto.String("Hello")}}
	_, payload, _ := buildEventPayload(context.Background(), nil, businessEvent("INT1", &waE2E.Message{InteractiveMessage: im}))
	if got := extractStructuredMessageContent(payload); got != formatInteractiveMessageSummary(im) {
		t.Fatalf("interactive content changed: %q", got)
	}
}

func TestBuildEventPayloadInteractiveTemplateCarouselAndQuickReplyID(t *testing.T) {
	card := func(body, id string) *waE2E.InteractiveMessage {
		return &waE2E.InteractiveMessage{
			Header: &waE2E.InteractiveMessage_Header{Title: proto.String("Offer"), Subtitle: proto.String("Today only"),
				Media: &waE2E.InteractiveMessage_Header_ImageMessage{ImageMessage: &waE2E.ImageMessage{}}},
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String(body)},
			InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
				Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
					Name:             proto.String("quick_reply"),
					ButtonParamsJSON: proto.String(`{"display_text":"Book","id":"` + id + `"}`),
				}},
			}},
		}
	}
	tm := &waE2E.TemplateMessage{Format: &waE2E.TemplateMessage_InteractiveMessageTemplate{
		InteractiveMessageTemplate: &waE2E.InteractiveMessage{
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String("Pick a package")},
			InteractiveMessage: &waE2E.InteractiveMessage_CarouselMessage_{CarouselMessage: &waE2E.InteractiveMessage_CarouselMessage{
				Cards: []*waE2E.InteractiveMessage{card("Two nights", "book-2"), card("Four nights", "book-4")},
			}},
		},
	}}
	_, payload, _ := buildEventPayload(context.Background(), nil, businessEvent("TPL5", &waE2E.Message{TemplateMessage: tm}))
	template := payload["template"].(webhookBusinessMessage)
	if len(template.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %+v", template.Cards)
	}
	second := template.Cards[1]
	if second.Body != "Four nights" || second.Subtitle != "Today only" || second.HeaderType != "image" {
		t.Fatalf("card rows not kept: %+v", second)
	}
	if len(second.Buttons) != 1 || second.Buttons[0].ID != "book-4" {
		t.Fatalf("quick reply id not kept: %+v", second.Buttons)
	}
	if got := extractStructuredMessageContent(payload); !strings.Contains(got, "Card 2: Offer") {
		t.Fatalf("cards missing from Chatwoot content: %q", got)
	}
}

func TestProductSummaryPrefersSalePrice(t *testing.T) {
	product := webhookProductPayload{Title: "Weekend package", CurrencyCode: "IDR", PriceAmount1000: 3500000, SalePriceAmount1000: 2900000}
	if got := formatProductSummary(product); got != "Product: Weekend package (IDR 2900.00)" {
		t.Fatalf("unexpected product summary %q", got)
	}
}

func TestNativeFlowSingleSelectKeepsRows(t *testing.T) {
	button := nativeFlowAsButton("single_select", `{"title":"Choose","sections":[{"title":"Services","rows":[{"id":"void","title":"Void PNR"},{"id":"status","title":"Booking status","description":"By PNR"}]}]}`, "")
	if button.Text != "Choose" || len(button.Rows) != 2 {
		t.Fatalf("rows not kept: %+v", button)
	}
	if button.Rows[1] != (webhookBusinessButtonRow{ID: "status", Title: "Booking status", Description: "By PNR"}) {
		t.Fatalf("unexpected row %+v", button.Rows[1])
	}
}

func TestBusinessSummaryFallsBackToButtonType(t *testing.T) {
	got := formatBusinessMessageSummary("Template message", webhookBusinessMessage{Buttons: []webhookBusinessButton{{Type: "review_and_pay"}}})
	if got != "[review_and_pay]" {
		t.Fatalf("unexpected summary %q", got)
	}
	if b := nativeFlowAsButton("single_select", `{"title":"Choose","id":"menu"}`, ""); b.ID != "menu" {
		t.Fatalf("single_select id not kept: %+v", b)
	}
}
