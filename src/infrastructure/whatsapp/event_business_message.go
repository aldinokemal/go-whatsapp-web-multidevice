package whatsapp

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// Template, buttons and product messages carry no Conversation or
// ExtendedTextMessage, so they used to reach webhooks empty. They are exposed
// as plain structs (not protos) so they survive the JSON round-trip on the
// Chatwoot retry path. payload["body"] is left untouched.

type webhookBusinessButton struct {
	Type  string                     `json:"type"` // quick_reply, url, call, copy, or the native-flow name
	Text  string                     `json:"text,omitempty"`
	ID    string                     `json:"id,omitempty"`
	URL   string                     `json:"url,omitempty"`
	Phone string                     `json:"phone_number,omitempty"`
	Code  string                     `json:"code,omitempty"`
	Rows  []webhookBusinessButtonRow `json:"rows,omitempty"` // single_select options
}

type webhookBusinessButtonRow struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type webhookBusinessMessage struct {
	Title      string                   `json:"title,omitempty"`
	Subtitle   string                   `json:"subtitle,omitempty"`
	HeaderType string                   `json:"header_type,omitempty"` // image, video, document, location, product
	Body       string                   `json:"body,omitempty"`
	Footer     string                   `json:"footer,omitempty"`
	TemplateID string                   `json:"template_id,omitempty"`
	Buttons    []webhookBusinessButton  `json:"buttons,omitempty"`
	Cards      []webhookBusinessMessage `json:"cards,omitempty"`
}

// Prices are in WhatsApp's unit (amount x 1000).
type webhookProductPayload struct {
	ProductID           string `json:"product_id,omitempty"`
	Title               string `json:"title,omitempty"`
	Description         string `json:"description,omitempty"`
	CurrencyCode        string `json:"currency_code,omitempty"`
	PriceAmount1000     int64  `json:"price_amount_1000,omitempty"`
	SalePriceAmount1000 int64  `json:"sale_price_amount_1000,omitempty"`
	RetailerID          string `json:"retailer_id,omitempty"`
	URL                 string `json:"url,omitempty"`
	CatalogTitle        string `json:"catalog_title,omitempty"`
	Body                string `json:"body,omitempty"`
	Footer              string `json:"footer,omitempty"`
	BusinessOwnerJID    string `json:"business_owner_jid,omitempty"`
}

// A tap on a list row, button or template quick reply. SelectedID matches the
// row or button id of the original message; list replies often have no title.
type webhookSelectionPayload struct {
	Kind        string `json:"kind"` // list, buttons, template
	Text        string `json:"text,omitempty"`
	Description string `json:"description,omitempty"`
	SelectedID  string `json:"selected_id,omitempty"`
}

func buildTemplatePayload(tm *waE2E.TemplateMessage) *webhookBusinessMessage {
	if tm == nil {
		return nil
	}
	hydrated := tm.GetHydratedTemplate()
	if hydrated == nil {
		hydrated = tm.GetHydratedFourRowTemplate()
	}
	if hydrated != nil {
		out := &webhookBusinessMessage{
			Title:      hydrated.GetHydratedTitleText(),
			HeaderType: templateHeaderType(hydrated),
			Body:       hydrated.GetHydratedContentText(),
			Footer:     hydrated.GetHydratedFooterText(),
			TemplateID: firstNonEmpty(tm.GetTemplateID(), hydrated.GetTemplateID()),
		}
		for _, button := range hydrated.GetHydratedButtons() {
			if b, ok := hydratedTemplateButton(button); ok {
				out.Buttons = append(out.Buttons, b)
			}
		}
		return out
	}
	if im := tm.GetInteractiveMessageTemplate(); im != nil {
		out := interactiveAsBusinessMessage(im)
		out.TemplateID = tm.GetTemplateID()
		return out
	}
	// A non-hydrated four-row template only has HSM element names.
	if tm.GetTemplateID() != "" {
		return &webhookBusinessMessage{TemplateID: tm.GetTemplateID()}
	}
	return nil
}

func templateHeaderType(t *waE2E.TemplateMessage_HydratedFourRowTemplate) string {
	switch {
	case t.GetImageMessage() != nil:
		return "image"
	case t.GetVideoMessage() != nil:
		return "video"
	case t.GetDocumentMessage() != nil:
		return "document"
	case t.GetLocationMessage() != nil:
		return "location"
	}
	return ""
}

func hydratedTemplateButton(button *waE2E.HydratedTemplateButton) (webhookBusinessButton, bool) {
	switch {
	case button.GetQuickReplyButton() != nil:
		quick := button.GetQuickReplyButton()
		return webhookBusinessButton{Type: "quick_reply", Text: quick.GetDisplayText(), ID: quick.GetID()}, true
	case button.GetUrlButton() != nil:
		link := button.GetUrlButton()
		return webhookBusinessButton{Type: "url", Text: link.GetDisplayText(), URL: link.GetURL()}, true
	case button.GetCallButton() != nil:
		call := button.GetCallButton()
		return webhookBusinessButton{Type: "call", Text: call.GetDisplayText(), Phone: call.GetPhoneNumber()}, true
	}
	return webhookBusinessButton{}, false
}

func buildButtonsPayload(bm *waE2E.ButtonsMessage) *webhookBusinessMessage {
	if bm == nil {
		return nil
	}
	out := &webhookBusinessMessage{
		Title:  bm.GetText(),
		Body:   bm.GetContentText(),
		Footer: bm.GetFooterText(),
	}
	switch {
	case bm.GetImageMessage() != nil:
		out.HeaderType = "image"
	case bm.GetVideoMessage() != nil:
		out.HeaderType = "video"
	case bm.GetDocumentMessage() != nil:
		out.HeaderType = "document"
	case bm.GetLocationMessage() != nil:
		out.HeaderType = "location"
	}
	for _, button := range bm.GetButtons() {
		if info := button.GetNativeFlowInfo(); info != nil {
			out.Buttons = append(out.Buttons, nativeFlowAsButton(info.GetName(), info.GetParamsJSON(), button.GetButtonID()))
			continue
		}
		out.Buttons = append(out.Buttons, webhookBusinessButton{
			Type: "quick_reply",
			Text: button.GetButtonText().GetDisplayText(),
			ID:   button.GetButtonID(),
		})
	}
	return out
}

// interactiveAsBusinessMessage maps an InteractiveMessage wrapped in a
// template onto the same shape. Carousel cards are mapped recursively.
func interactiveAsBusinessMessage(im *waE2E.InteractiveMessage) *webhookBusinessMessage {
	header := im.GetHeader()
	out := &webhookBusinessMessage{
		Title:      header.GetTitle(),
		Subtitle:   header.GetSubtitle(),
		HeaderType: interactiveHeaderType(header),
		Body:       im.GetBody().GetText(),
		Footer:     im.GetFooter().GetText(),
	}
	for _, button := range im.GetNativeFlowMessage().GetButtons() {
		out.Buttons = append(out.Buttons, nativeFlowAsButton(button.GetName(), button.GetButtonParamsJSON(), ""))
	}
	for _, card := range im.GetCarouselMessage().GetCards() {
		out.Cards = append(out.Cards, *interactiveAsBusinessMessage(card))
	}
	return out
}

func interactiveHeaderType(h *waE2E.InteractiveMessage_Header) string {
	switch {
	case h.GetImageMessage() != nil:
		return "image"
	case h.GetVideoMessage() != nil:
		return "video"
	case h.GetDocumentMessage() != nil:
		return "document"
	case h.GetLocationMessage() != nil:
		return "location"
	case h.GetProductMessage() != nil:
		return "product"
	}
	return ""
}

func nativeFlowAsButton(name, paramsJSON, fallbackID string) webhookBusinessButton {
	var params nativeFlowButtonParams
	_ = json.Unmarshal([]byte(paramsJSON), &params)
	text := firstNonEmpty(params.DisplayText, params.Title)
	switch name {
	case "cta_url":
		return webhookBusinessButton{Type: "url", Text: text, URL: params.URL}
	case "cta_call":
		return webhookBusinessButton{Type: "call", Text: text, Phone: params.PhoneNumber}
	case "cta_copy":
		return webhookBusinessButton{Type: "copy", Text: text, Code: params.Copy}
	case "quick_reply":
		return webhookBusinessButton{Type: "quick_reply", Text: text, ID: firstNonEmpty(params.ID, fallbackID)}
	case "single_select":
		button := webhookBusinessButton{Type: name, Text: text, ID: firstNonEmpty(params.ID, fallbackID)}
		for _, section := range params.Sections {
			for _, row := range section.Rows {
				button.Rows = append(button.Rows, webhookBusinessButtonRow{ID: row.ID, Title: row.Title, Description: row.Description})
			}
		}
		return button
	}
	return webhookBusinessButton{Type: name, Text: text, ID: firstNonEmpty(params.ID, fallbackID)}
}

func buildProductPayload(pm *waE2E.ProductMessage) *webhookProductPayload {
	if pm == nil {
		return nil
	}
	product := pm.GetProduct()
	return &webhookProductPayload{
		ProductID:           product.GetProductID(),
		Title:               product.GetTitle(),
		Description:         product.GetDescription(),
		CurrencyCode:        product.GetCurrencyCode(),
		PriceAmount1000:     product.GetPriceAmount1000(),
		SalePriceAmount1000: product.GetSalePriceAmount1000(),
		RetailerID:          product.GetRetailerID(),
		URL:                 product.GetURL(),
		CatalogTitle:        pm.GetCatalog().GetTitle(),
		Body:                pm.GetBody(),
		Footer:              pm.GetFooter(),
		BusinessOwnerJID:    pm.GetBusinessOwnerJID(),
	}
}

func buildSelectionPayload(msg *waE2E.Message) *webhookSelectionPayload {
	switch {
	case msg.GetListResponseMessage() != nil:
		reply := msg.GetListResponseMessage()
		return &webhookSelectionPayload{
			Kind:        "list",
			Text:        reply.GetTitle(),
			Description: reply.GetDescription(),
			SelectedID:  reply.GetSingleSelectReply().GetSelectedRowID(),
		}
	case msg.GetButtonsResponseMessage() != nil:
		reply := msg.GetButtonsResponseMessage()
		return &webhookSelectionPayload{Kind: "buttons", Text: reply.GetSelectedDisplayText(), SelectedID: reply.GetSelectedButtonID()}
	case msg.GetTemplateButtonReplyMessage() != nil:
		reply := msg.GetTemplateButtonReplyMessage()
		return &webhookSelectionPayload{Kind: "template", Text: reply.GetSelectedDisplayText(), SelectedID: reply.GetSelectedID()}
	}
	return nil
}

// formatBusinessMessageSummary renders a template or buttons payload for
// Chatwoot, with the same button glyphs as formatInteractiveMessageSummary.
// value is the struct on the live path and a map after a retry.
func formatBusinessMessageSummary(label string, value any) string {
	var message webhookBusinessMessage
	if !decodeWebhookValue(value, &message) {
		return ""
	}
	if summary := businessMessageLines(message); summary != "" {
		return summary
	}
	return label
}

func businessMessageLines(message webhookBusinessMessage) string {
	var parts []string
	for _, row := range []string{message.Title, message.Subtitle, message.Body, message.Footer} {
		if row != "" {
			parts = append(parts, row)
		}
	}
	for _, button := range message.Buttons {
		switch {
		case button.Type == "url" && button.URL != "":
			parts = append(parts, fmt.Sprintf("🔗 %s: %s", firstNonEmpty(button.Text, "Link"), button.URL))
		case button.Type == "call" && button.Phone != "":
			parts = append(parts, fmt.Sprintf("📞 %s: %s", firstNonEmpty(button.Text, "Call"), button.Phone))
		case button.Type == "copy" && button.Code != "":
			parts = append(parts, fmt.Sprintf("📋 %s: %s", firstNonEmpty(button.Text, "Copy code"), button.Code))
		case button.Text != "" || button.Type != "":
			parts = append(parts, fmt.Sprintf("[%s]", firstNonEmpty(button.Text, button.Type)))
		}
	}
	for i, card := range message.Cards {
		if lines := businessMessageLines(card); lines != "" {
			parts = append(parts, fmt.Sprintf("Card %d: %s", i+1, lines))
		}
	}
	return strings.Join(parts, "\n")
}

func formatProductSummary(value any) string {
	var product webhookProductPayload
	if !decodeWebhookValue(value, &product) {
		return ""
	}
	title := firstNonEmpty(product.Title, product.Body, "Product")
	price := product.PriceAmount1000
	if product.SalePriceAmount1000 > 0 {
		price = product.SalePriceAmount1000
	}
	if price > 0 && product.CurrencyCode != "" {
		return fmt.Sprintf("Product: %s (%s %.2f)", title, product.CurrencyCode, float64(price)/1000)
	}
	return "Product: " + title
}

func formatSelectionSummary(value any) string {
	var selection webhookSelectionPayload
	if !decodeWebhookValue(value, &selection) {
		return ""
	}
	if selection.Text != "" {
		return "Selected: " + selection.Text
	}
	if selection.SelectedID != "" {
		return "Selected option " + selection.SelectedID
	}
	return ""
}

func decodeWebhookValue(value any, target any) bool {
	if value == nil {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
