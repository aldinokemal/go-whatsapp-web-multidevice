package utils

import (
	"reflect"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

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

const hydratedOrderTemplateText = "Order confirmed\nHi John, your order #1234 has shipped.\nAcme Store\n" +
	"🔗 Track order: https://acme.example/track/1234\n📞 Call us: +15550100\n[Stop updates]"

func interactiveCopyCodeMessage() *waE2E.InteractiveMessage {
	return &waE2E.InteractiveMessage{
		Header: &waE2E.InteractiveMessage_Header{Title: proto.String("Promo")},
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("20% off today")},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
			Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
				Name:             proto.String("cta_copy"),
				ButtonParamsJSON: proto.String(`{"display_text":"Copy code","copy_code":"SAVE20"}`),
			}},
		}},
	}
}

// A template may carry a hydrated fallback next to its interactive form; the
// text and the webhook payload must describe the same one.
func interactiveTemplateWithHydratedFallback() *waE2E.Message {
	return &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
		HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{HydratedContentText: proto.String("fallback"), TemplateID: proto.String("promo")},
		Format:           &waE2E.TemplateMessage_InteractiveMessageTemplate{InteractiveMessageTemplate: interactiveCopyCodeMessage()},
	}}
}

// A native-flow tap can carry its choice only in paramsJSON.
func nativeFlowReplyWithoutBody() *waE2E.Message {
	return &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
		InteractiveResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage_{
			NativeFlowResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage{
				Name: proto.String("quick_reply"), ParamsJSON: proto.String(`{"id":"book_2"}`),
			},
		},
	}}
}

func listReplyWithoutTitle() *waE2E.Message {
	return &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{
		ListType:          waE2E.ListResponseMessage_SINGLE_SELECT.Enum(),
		SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String("row-void-pnr")},
	}}
}

func TestExtractMessageTextFromProtoBusinessMessages(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{
			name: "HydratedTemplateKeepsEveryRowAndButton",
			msg:  &waE2E.Message{TemplateMessage: hydratedOrderTemplate()},
			want: hydratedOrderTemplateText,
		},
		{
			name: "HydratedFourRowTemplateOneof",
			msg: &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{Format: &waE2E.TemplateMessage_HydratedFourRowTemplate_{
				HydratedFourRowTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{HydratedContentText: proto.String("Your code is 482913")},
			}}},
			want: "Your code is 482913",
		},
		{
			// History sync delivers the raw, still-wrapped message.
			name: "EphemeralWrappedTemplate",
			msg:  &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{TemplateMessage: hydratedOrderTemplate()}}},
			want: hydratedOrderTemplateText,
		},
		{
			name: "TemplateWithOnlyMediaHeaderGetsPlaceholder",
			msg: &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
				Title: &waE2E.TemplateMessage_HydratedFourRowTemplate_ImageMessage{ImageMessage: &waE2E.ImageMessage{}},
			}}},
			want: "Template message",
		},
		{
			name: "NonHydratedFourRowTemplateGetsPlaceholder",
			msg: &waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{
				TemplateID: proto.String("otp"),
				Format:     &waE2E.TemplateMessage_FourRowTemplate_{FourRowTemplate: &waE2E.TemplateMessage_FourRowTemplate{}},
			}},
			want: "Template message",
		},
		{
			name: "TemplateWrappingInteractiveRendersLikeInteractive",
			msg:  interactiveTemplateWithHydratedFallback(),
			want: "Promo\n20% off today\n📋 Copy code: SAVE20",
		},
		{
			name: "HydratedHighlyStructuredMessage",
			msg: &waE2E.Message{HighlyStructuredMessage: &waE2E.HighlyStructuredMessage{
				ElementName: proto.String("order_confirmation"),
				HydratedHsm: &waE2E.TemplateMessage{HydratedTemplate: &waE2E.TemplateMessage_HydratedFourRowTemplate{
					HydratedContentText: proto.String("Thanks for your order"),
				}},
			}},
			want: "Thanks for your order",
		},
		{
			// A non-hydrated HSM only carries an element name and params.
			name: "NonHydratedHighlyStructuredMessageStaysEmpty",
			msg: &waE2E.Message{HighlyStructuredMessage: &waE2E.HighlyStructuredMessage{
				ElementName: proto.String("order_confirmation"),
				Params:      []string{"42"},
			}},
			want: "",
		},
		{
			name: "InteractiveMessage",
			msg: &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
				Header: &waE2E.InteractiveMessage_Header{Title: proto.String("Support")},
				Body:   &waE2E.InteractiveMessage_Body{Text: proto.String("How can we help?")},
				Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String("Acme Support")},
				InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
					Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
						Name:             proto.String("cta_url"),
						ButtonParamsJSON: proto.String(`{"display_text":"Visit site","url":"https://acme.example"}`),
					}},
				}},
			}},
			want: "Support\nHow can we help?\nAcme Support\n🔗 Visit site: https://acme.example",
		},
		{
			name: "ViewOnceWrappedInteractive",
			msg: &waE2E.Message{ViewOnceMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{InteractiveMessage: &waE2E.InteractiveMessage{
				Body: &waE2E.InteractiveMessage_Body{Text: proto.String("How can we help?")},
			}}}},
			want: "How can we help?",
		},
		{
			name: "InteractiveResponse",
			msg: &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
				Body: &waE2E.InteractiveResponseMessage_Body{Text: proto.String("Track my order")},
			}},
			want: "Track my order",
		},
		{
			name: "NativeFlowReplyWithOnlyAnID",
			msg:  nativeFlowReplyWithoutBody(),
			want: "Selected option book_2",
		},
		{
			name: "EmptySelectionReplyGetsPlaceholder",
			msg:  &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{}},
			want: "Selection message",
		},
		{
			name: "ButtonsMessage",
			msg: &waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{
				Header:      &waE2E.ButtonsMessage_Text{Text: "Travel desk"},
				ContentText: proto.String("Please choose the service"),
				FooterText:  proto.String("Reply with a button"),
				Buttons: []*waE2E.ButtonsMessage_Button{
					{ButtonID: proto.String("sales"), ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String("Sales")}},
					{ButtonID: proto.String("support"), ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String("Support")}},
				},
			}},
			want: "Travel desk\nPlease choose the service\nReply with a button\n[Sales]\n[Support]",
		},
		{
			name: "ButtonsMessageWithoutTextGetsPlaceholder",
			msg:  &waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{}},
			want: "Buttons message",
		},
		{
			name: "ListMessage",
			msg: &waE2E.Message{ListMessage: &waE2E.ListMessage{
				Title:       proto.String("Menu"),
				Description: proto.String("Choose a product"),
				FooterText:  proto.String("Acme"),
				Sections: []*waE2E.ListMessage_Section{{Rows: []*waE2E.ListMessage_Row{
					{Title: proto.String("Mug"), RowID: proto.String("mug")},
					{RowID: proto.String("untitled")},
					{Title: proto.String("Shirt"), RowID: proto.String("shirt")},
				}}},
			}},
			want: "List: Menu\nChoose a product\nAcme\n[Mug]\n[Shirt]",
		},
		{
			name: "ListMessageWithoutTextGetsPlaceholder",
			msg:  &waE2E.Message{ListMessage: &waE2E.ListMessage{}},
			want: "List message",
		},
		{
			name: "ProductMessagePrefersSalePrice",
			msg: &waE2E.Message{ProductMessage: &waE2E.ProductMessage{
				Body: proto.String("Limited offer"),
				Product: &waE2E.ProductMessage_ProductSnapshot{
					Title: proto.String("Weekend package"), CurrencyCode: proto.String("IDR"),
					PriceAmount1000: proto.Int64(3500000), SalePriceAmount1000: proto.Int64(2900000),
				},
			}},
			want: "Product: Weekend package (IDR 2900.00)\nLimited offer",
		},
		{
			name: "ProductMessageWithoutText",
			msg:  &waE2E.Message{ProductMessage: &waE2E.ProductMessage{}},
			want: "Product",
		},
		{
			name: "OrderMessage",
			msg: &waE2E.Message{OrderMessage: &waE2E.OrderMessage{
				Message:    proto.String("2 items - $30.00"),
				OrderTitle: proto.String("Order #42"),
			}},
			want: "Order: Order #42\n2 items - $30.00",
		},
		{
			name: "OrderMessageWithoutTextGetsPlaceholder",
			msg:  &waE2E.Message{OrderMessage: &waE2E.OrderMessage{}},
			want: "Order message",
		},
		{
			name: "ListReplyTitle",
			msg: &waE2E.Message{ListResponseMessage: &waE2E.ListResponseMessage{
				Title:             proto.String("Void PNR"),
				SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String("row-void-pnr")},
			}},
			want: "Void PNR",
		},
		{
			name: "ListReplyWithOnlyRowID",
			msg:  listReplyWithoutTitle(),
			want: "Selected option row-void-pnr",
		},
		{
			name: "ButtonsReply",
			msg: &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
				SelectedButtonID: proto.String("support"),
				Response:         &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Support"},
			}},
			want: "Support",
		},
		{
			name: "TemplateButtonReply",
			msg: &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
				SelectedID: proto.String("stop"), SelectedDisplayText: proto.String("Stop updates"),
			}},
			want: "Stop updates",
		},
		{
			name: "UnknownTypeStaysEmpty",
			msg:  &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{Text: proto.String("👍")}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractMessageTextFromProto(tt.msg); got != tt.want {
				t.Fatalf("ExtractMessageTextFromProto() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The webhook body carries the business text too, replies included.
func TestBuildEventMessageUsesBusinessText(t *testing.T) {
	tests := []struct {
		name string
		msg  *waE2E.Message
		want string
	}{
		{"template", &waE2E.Message{TemplateMessage: hydratedOrderTemplate()}, hydratedOrderTemplateText},
		{"list reply", listReplyWithoutTitle(), "Selected option row-void-pnr"},
		{"buttons reply", &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
			Response: &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: "Support"},
		}}, "Support"},
		{"template reply", &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
			SelectedDisplayText: proto.String("Stop updates"),
		}}, "Stop updates"},
	}
	for _, tt := range tests {
		if got := BuildEventMessage(&events.Message{Info: types.MessageInfo{ID: "MSG1"}, Message: tt.msg}).Text; got != tt.want {
			t.Errorf("%s: BuildEventMessage().Text = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestExtractMediaInfoUnwrapsWrappers(t *testing.T) {
	msg := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption:    proto.String("look at this"),
		DirectPath: proto.String("/v/t62/abc"),
	}}}}
	mediaType, _, _, directPath, _, _, _, _ := ExtractMediaInfo(msg)
	if mediaType != "image" || directPath != "/v/t62/abc" {
		t.Fatalf("wrapped image lost its media info: type=%q direct_path=%q", mediaType, directPath)
	}
}

func TestBuildTemplatePayloadKeepsAllRows(t *testing.T) {
	template := BuildTemplatePayload(&waE2E.Message{TemplateMessage: hydratedOrderTemplate()})
	if template == nil {
		t.Fatal("expected template payload")
	}
	if template.Title != "Order confirmed" || template.Body != "Hi John, your order #1234 has shipped." ||
		template.Footer != "Acme Store" || template.TemplateID != "order_confirmed" {
		t.Fatalf("rows not kept: %+v", template)
	}
	want := []BusinessButton{
		{Type: "url", Text: "Track order", URL: "https://acme.example/track/1234"},
		{Type: "call", Text: "Call us", Phone: "+15550100"},
		{Type: "quick_reply", Text: "Stop updates", ID: "stop"},
	}
	if !reflect.DeepEqual(template.Buttons, want) {
		t.Fatalf("buttons = %+v, want %+v", template.Buttons, want)
	}
}

func TestBuildTemplatePayloadWrappingInteractive(t *testing.T) {
	template := BuildTemplatePayload(interactiveTemplateWithHydratedFallback())
	if template.Title != "Promo" || template.Body != "20% off today" || template.TemplateID != "promo" {
		t.Fatalf("interactive rows not kept: %+v", template)
	}
	if want := []BusinessButton{{Type: "copy", Text: "Copy code", Code: "SAVE20"}}; !reflect.DeepEqual(template.Buttons, want) {
		t.Fatalf("buttons = %+v, want %+v", template.Buttons, want)
	}
}

func TestBuildTemplatePayloadCarouselKeepsCardsAndQuickReplyIDs(t *testing.T) {
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
	template := BuildTemplatePayload(&waE2E.Message{TemplateMessage: &waE2E.TemplateMessage{Format: &waE2E.TemplateMessage_InteractiveMessageTemplate{
		InteractiveMessageTemplate: &waE2E.InteractiveMessage{
			Body: &waE2E.InteractiveMessage_Body{Text: proto.String("Pick a package")},
			InteractiveMessage: &waE2E.InteractiveMessage_CarouselMessage_{CarouselMessage: &waE2E.InteractiveMessage_CarouselMessage{
				Cards: []*waE2E.InteractiveMessage{card("Two nights", "book-2"), card("Four nights", "book-4")},
			}},
		},
	}}})
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
}

func TestBuildButtonsPayload(t *testing.T) {
	buttons := BuildButtonsPayload(&waE2E.Message{ButtonsMessage: &waE2E.ButtonsMessage{
		Header:      &waE2E.ButtonsMessage_Text{Text: "Travel desk"},
		ContentText: proto.String("Please choose the service"),
		Buttons: []*waE2E.ButtonsMessage_Button{
			{ButtonID: proto.String("support"), ButtonText: &waE2E.ButtonsMessage_Button_ButtonText{DisplayText: proto.String("Support")}},
			{ButtonID: proto.String("menu"), NativeFlowInfo: &waE2E.ButtonsMessage_Button_NativeFlowInfo{
				Name: proto.String("single_select"), ParamsJSON: proto.String(`{"title":"Choose"}`),
			}},
		},
	}})
	if buttons.Title != "Travel desk" || buttons.Body != "Please choose the service" {
		t.Fatalf("rows not kept: %+v", buttons)
	}
	want := []BusinessButton{
		{Type: "quick_reply", Text: "Support", ID: "support"},
		{Type: "single_select", Text: "Choose", ID: "menu"},
	}
	if !reflect.DeepEqual(buttons.Buttons, want) {
		t.Fatalf("buttons = %+v, want %+v", buttons.Buttons, want)
	}
}

func TestBuildProductPayload(t *testing.T) {
	product := BuildProductPayload(&waE2E.Message{ProductMessage: &waE2E.ProductMessage{
		Product: &waE2E.ProductMessage_ProductSnapshot{
			ProductID: proto.String("p-77"), Title: proto.String("Weekend package"),
			CurrencyCode: proto.String("IDR"), PriceAmount1000: proto.Int64(3500000),
		},
		BusinessOwnerJID: proto.String("628111222333@s.whatsapp.net"),
		Catalog:          &waE2E.ProductMessage_CatalogSnapshot{Title: proto.String("Packages")},
	}})
	if product.ProductID != "p-77" || product.PriceAmount1000 != 3500000 || product.CatalogTitle != "Packages" ||
		product.BusinessOwnerJID != "628111222333@s.whatsapp.net" {
		t.Fatalf("product not kept: %+v", product)
	}
}

func TestBuildSelectionPayloadListReplyWithoutTitle(t *testing.T) {
	selection := BuildSelectionPayload(listReplyWithoutTitle())
	if selection == nil || selection.Kind != "list" || selection.SelectedID != "row-void-pnr" {
		t.Fatalf("selected row not kept: %+v", selection)
	}
}

func TestBuildSelectionPayloadNativeFlowReply(t *testing.T) {
	selection := BuildSelectionPayload(nativeFlowReplyWithoutBody())
	if selection == nil || selection.Kind != "interactive" || selection.SelectedID != "book_2" {
		t.Fatalf("native-flow reply not kept: %+v", selection)
	}
}

func TestNativeFlowAsButtonKeepsSingleSelectRows(t *testing.T) {
	button := nativeFlowAsButton("single_select", `{"title":"Choose","sections":[{"title":"Services","rows":[{"id":"void","title":"Void PNR"},{"id":"status","title":"Booking status","description":"By PNR"}]}]}`, "")
	want := BusinessButton{Type: "single_select", Text: "Choose", Rows: []BusinessButtonRow{
		{ID: "void", Title: "Void PNR"},
		{ID: "status", Title: "Booking status", Description: "By PNR"},
	}}
	if !reflect.DeepEqual(button, want) {
		t.Fatalf("button = %+v, want %+v", button, want)
	}
}

func TestBusinessMessageLinesFallsBackToButtonType(t *testing.T) {
	if got := businessMessageLines(&BusinessMessage{Buttons: []BusinessButton{{Type: "review_and_pay"}}}); got != "[review_and_pay]" {
		t.Fatalf("unexpected lines %q", got)
	}
}
