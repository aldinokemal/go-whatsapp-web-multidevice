package utils

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

// WhatsApp Business / Cloud API senders put their text in template, buttons,
// interactive, list, product and order messages instead of Conversation or
// ExtendedTextMessage. The Build*Payload helpers expose them to webhooks as
// plain structs (not protos, so they survive a JSON round-trip), and
// extractBusinessMessageText renders the same data as the text used for the
// webhook body, stored message content and Chatwoot.

type BusinessButton struct {
	Type  string              `json:"type"` // quick_reply, url, call, copy, or the native-flow name
	Text  string              `json:"text,omitempty"`
	ID    string              `json:"id,omitempty"`
	URL   string              `json:"url,omitempty"`
	Phone string              `json:"phone_number,omitempty"`
	Code  string              `json:"code,omitempty"`
	Rows  []BusinessButtonRow `json:"rows,omitempty"` // single_select options
}

type BusinessButtonRow struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// BusinessMessage is a template or buttons message for webhooks.
type BusinessMessage struct {
	Title      string            `json:"title,omitempty"`
	Subtitle   string            `json:"subtitle,omitempty"`
	HeaderType string            `json:"header_type,omitempty"` // image, video, document, location, product
	Body       string            `json:"body,omitempty"`
	Footer     string            `json:"footer,omitempty"`
	TemplateID string            `json:"template_id,omitempty"`
	Buttons    []BusinessButton  `json:"buttons,omitempty"`
	Cards      []BusinessMessage `json:"cards,omitempty"`
}

// BusinessProduct is a ProductMessage for webhooks. Prices are in WhatsApp's
// unit (amount x 1000).
type BusinessProduct struct {
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

// BusinessSelection is a tap on a list row, button, template quick reply or
// native-flow button. SelectedID matches the row or button id of the original
// message.
type BusinessSelection struct {
	Kind        string `json:"kind"` // list, buttons, template, interactive
	Text        string `json:"text,omitempty"`
	Description string `json:"description,omitempty"`
	SelectedID  string `json:"selected_id,omitempty"`
}

// templateMessage returns the template carried by msg. Legacy senders wrap
// it in a HighlyStructuredMessage; only the hydrated form carries text.
func templateMessage(msg *waE2E.Message) *waE2E.TemplateMessage {
	return cmp.Or(msg.GetTemplateMessage(), msg.GetHighlyStructuredMessage().GetHydratedHsm())
}

// BuildTemplatePayload returns the template (or hydrated HSM) in msg as a
// webhook struct, or nil.
func BuildTemplatePayload(msg *waE2E.Message) *BusinessMessage {
	tm := templateMessage(msg)
	if tm == nil {
		return nil
	}
	hydrated := cmp.Or(tm.GetHydratedTemplate(), tm.GetHydratedFourRowTemplate())
	templateID := cmp.Or(tm.GetTemplateID(), hydrated.GetTemplateID())
	if im := tm.GetInteractiveMessageTemplate(); im != nil {
		out := interactiveAsBusinessMessage(im)
		out.TemplateID = templateID
		return out
	}
	if hydrated != nil {
		out := &BusinessMessage{
			Title:      hydrated.GetHydratedTitleText(),
			HeaderType: templateHeaderType(hydrated),
			Body:       hydrated.GetHydratedContentText(),
			Footer:     hydrated.GetHydratedFooterText(),
			TemplateID: templateID,
		}
		for _, button := range hydrated.GetHydratedButtons() {
			if b, ok := hydratedTemplateButton(button); ok {
				out.Buttons = append(out.Buttons, b)
			}
		}
		return out
	}
	// A non-hydrated four-row template only has HSM element names.
	if templateID != "" {
		return &BusinessMessage{TemplateID: templateID}
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

func hydratedTemplateButton(button *waE2E.HydratedTemplateButton) (BusinessButton, bool) {
	switch {
	case button.GetQuickReplyButton() != nil:
		quick := button.GetQuickReplyButton()
		return BusinessButton{Type: "quick_reply", Text: quick.GetDisplayText(), ID: quick.GetID()}, true
	case button.GetUrlButton() != nil:
		link := button.GetUrlButton()
		return BusinessButton{Type: "url", Text: link.GetDisplayText(), URL: link.GetURL()}, true
	case button.GetCallButton() != nil:
		call := button.GetCallButton()
		return BusinessButton{Type: "call", Text: call.GetDisplayText(), Phone: call.GetPhoneNumber()}, true
	}
	return BusinessButton{}, false
}

// BuildButtonsPayload returns the ButtonsMessage in msg as a webhook struct,
// or nil.
func BuildButtonsPayload(msg *waE2E.Message) *BusinessMessage {
	bm := msg.GetButtonsMessage()
	if bm == nil {
		return nil
	}
	out := &BusinessMessage{
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
		out.Buttons = append(out.Buttons, BusinessButton{
			Type: "quick_reply",
			Text: button.GetButtonText().GetDisplayText(),
			ID:   button.GetButtonID(),
		})
	}
	return out
}

// interactiveAsBusinessMessage maps an InteractiveMessage wrapped in a
// template onto the same shape. Carousel cards are mapped recursively.
func interactiveAsBusinessMessage(im *waE2E.InteractiveMessage) *BusinessMessage {
	header := im.GetHeader()
	out := &BusinessMessage{
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

func nativeFlowAsButton(name, paramsJSON, fallbackID string) BusinessButton {
	var params nativeFlowButtonParams
	_ = json.Unmarshal([]byte(paramsJSON), &params)
	text := cmp.Or(params.DisplayText, params.Title)
	switch name {
	case "cta_url":
		return BusinessButton{Type: "url", Text: text, URL: params.URL}
	case "cta_call":
		return BusinessButton{Type: "call", Text: text, Phone: params.PhoneNumber}
	case "cta_copy":
		return BusinessButton{Type: "copy", Text: text, Code: params.Copy}
	}
	button := BusinessButton{Type: name, Text: text, ID: cmp.Or(params.ID, fallbackID)}
	for _, section := range params.Sections {
		for _, row := range section.Rows {
			button.Rows = append(button.Rows, BusinessButtonRow{ID: row.ID, Title: row.Title, Description: row.Description})
		}
	}
	return button
}

// BuildProductPayload returns the ProductMessage in msg as a webhook struct,
// or nil.
func BuildProductPayload(msg *waE2E.Message) *BusinessProduct {
	pm := msg.GetProductMessage()
	if pm == nil {
		return nil
	}
	product := pm.GetProduct()
	return &BusinessProduct{
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

// BuildSelectionPayload returns the list, buttons, template or native-flow
// reply in msg, or nil.
func BuildSelectionPayload(msg *waE2E.Message) *BusinessSelection {
	switch {
	case msg.GetListResponseMessage() != nil:
		reply := msg.GetListResponseMessage()
		return &BusinessSelection{
			Kind:        "list",
			Text:        reply.GetTitle(),
			Description: reply.GetDescription(),
			SelectedID:  reply.GetSingleSelectReply().GetSelectedRowID(),
		}
	case msg.GetButtonsResponseMessage() != nil:
		reply := msg.GetButtonsResponseMessage()
		return &BusinessSelection{Kind: "buttons", Text: reply.GetSelectedDisplayText(), SelectedID: reply.GetSelectedButtonID()}
	case msg.GetTemplateButtonReplyMessage() != nil:
		reply := msg.GetTemplateButtonReplyMessage()
		return &BusinessSelection{Kind: "template", Text: reply.GetSelectedDisplayText(), SelectedID: reply.GetSelectedID()}
	case msg.GetInteractiveResponseMessage() != nil:
		reply := msg.GetInteractiveResponseMessage()
		var params nativeFlowButtonParams
		_ = json.Unmarshal([]byte(reply.GetNativeFlowResponseMessage().GetParamsJSON()), &params)
		return &BusinessSelection{Kind: "interactive", Text: reply.GetBody().GetText(), SelectedID: params.ID}
	}
	return nil
}

// extractBusinessMessageText renders a business message, or a reply to one,
// as plain text. Messages without any text get a placeholder so they still
// show up in the chat instead of being skipped by storage.
func extractBusinessMessageText(msg *waE2E.Message) string {
	if tm := templateMessage(msg); tm != nil {
		if im := tm.GetInteractiveMessageTemplate(); im != nil {
			return FormatInteractiveMessageSummary(im)
		}
		return cmp.Or(businessMessageLines(BuildTemplatePayload(msg)), "Template message")
	}
	if im := msg.GetInteractiveMessage(); im != nil {
		return FormatInteractiveMessageSummary(im)
	}
	if buttons := BuildButtonsPayload(msg); buttons != nil {
		return cmp.Or(businessMessageLines(buttons), "Buttons message")
	}
	if list := msg.GetListMessage(); list != nil {
		title := list.GetTitle()
		if title != "" {
			title = "List: " + title
		}
		rendered := &BusinessMessage{Title: title, Body: list.GetDescription(), Footer: list.GetFooterText()}
		for _, section := range list.GetSections() {
			for _, row := range section.GetRows() {
				rendered.Buttons = append(rendered.Buttons, BusinessButton{Text: row.GetTitle()})
			}
		}
		return cmp.Or(businessMessageLines(rendered), "List message")
	}
	if product := BuildProductPayload(msg); product != nil {
		line := "Product"
		if product.Title != "" {
			line += ": " + product.Title
		}
		if price := cmp.Or(product.SalePriceAmount1000, product.PriceAmount1000); price > 0 && product.CurrencyCode != "" {
			line += fmt.Sprintf(" (%s %.2f)", product.CurrencyCode, float64(price)/1000)
		}
		return joinNonEmptyLines(line, product.Body, product.Footer)
	}
	if order := msg.GetOrderMessage(); order != nil {
		title := order.GetOrderTitle()
		if title != "" {
			title = "Order: " + title
		}
		return cmp.Or(joinNonEmptyLines(title, order.GetMessage()), "Order message")
	}
	if selection := BuildSelectionPayload(msg); selection != nil {
		text := cmp.Or(selection.Text, selection.Description)
		// Recent clients send list and native-flow replies with only the id.
		if text == "" && selection.SelectedID != "" {
			text = "Selected option " + selection.SelectedID
		}
		return cmp.Or(text, "Selection message")
	}
	return ""
}

// businessMessageLines renders the rows, then one line per button: the same
// 🔗/📞/📋 lines as FormatInteractiveMessageSummary, other buttons as [label].
func businessMessageLines(message *BusinessMessage) string {
	if message == nil {
		return ""
	}
	parts := []string{message.Title, message.Body, message.Footer}
	for _, button := range message.Buttons {
		switch {
		case button.Type == "url" && button.URL != "":
			parts = append(parts, fmt.Sprintf("🔗 %s: %s", cmp.Or(button.Text, "Link"), button.URL))
		case button.Type == "call" && button.Phone != "":
			parts = append(parts, fmt.Sprintf("📞 %s: %s", cmp.Or(button.Text, "Call"), button.Phone))
		case button.Type == "copy" && button.Code != "":
			parts = append(parts, fmt.Sprintf("📋 %s: %s", cmp.Or(button.Text, "Copy code"), button.Code))
		case button.Text != "" || button.Type != "":
			parts = append(parts, fmt.Sprintf("[%s]", cmp.Or(button.Text, button.Type)))
		}
	}
	return joinNonEmptyLines(parts...)
}

func joinNonEmptyLines(values ...string) string {
	var lines []string
	for _, value := range values {
		if value != "" {
			lines = append(lines, value)
		}
	}
	return strings.Join(lines, "\n")
}

// FormatInteractiveMessageSummary renders an InteractiveMessage (business/
// Cloud API messages with native buttons: cta_url, cta_call, single/multi
// select, etc.) as plain text, since neither chat storage nor Chatwoot has a
// native concept of a WhatsApp interactive button. Buttons are described in
// buttonParamsJSON as a per-button-type JSON blob (undocumented,
// reverse-engineered from traffic), so only the fields relevant to rendering
// are decoded and anything unrecognized still shows the button's raw name.
func FormatInteractiveMessageSummary(im *waE2E.InteractiveMessage) string {
	var parts []string

	if header := im.GetHeader(); header != nil {
		if title := header.GetTitle(); title != "" {
			parts = append(parts, title)
		}
		if subtitle := header.GetSubtitle(); subtitle != "" {
			parts = append(parts, subtitle)
		}
		// ExtractMediaCaption only handles top-level Get*Message() cases, not
		// InteractiveMessage.Header — surface header captions here, since
		// collectInteractiveMedia forwards the file itself but nothing else
		// carries the caption text.
		if caption := interactiveHeaderMediaCaption(header); caption != "" {
			parts = append(parts, caption)
		}
	}
	if body := im.GetBody(); body != nil {
		if text := body.GetText(); text != "" {
			parts = append(parts, text)
		}
	}
	if footer := im.GetFooter(); footer != nil {
		if text := footer.GetText(); text != "" {
			parts = append(parts, text)
		}
	}

	for _, button := range im.GetNativeFlowMessage().GetButtons() {
		if line := formatNativeFlowButton(button); line != "" {
			parts = append(parts, line)
		}
	}

	// Carousels put their CTA/quick-reply buttons on each card rather than on
	// the top-level NativeFlowMessage, so the loop above sees none of them —
	// summarize each card (itself a full InteractiveMessage) separately.
	if carousel := im.GetCarouselMessage(); carousel != nil {
		for i, card := range carousel.GetCards() {
			if cardSummary := FormatInteractiveMessageSummary(card); cardSummary != "" && cardSummary != "Interactive message" {
				parts = append(parts, fmt.Sprintf("Card %d: %s", i+1, cardSummary))
			}
		}
	}

	if len(parts) == 0 {
		return "Interactive message"
	}
	return strings.Join(parts, "\n")
}

// interactiveHeaderMediaCaption returns the caption on whichever media type
// (if any) an InteractiveMessage header carries. Mirrors ExtractMediaCaption's
// per-type checks, scoped to the Header oneof instead of top-level Get*Message().
func interactiveHeaderMediaCaption(header *waE2E.InteractiveMessage_Header) string {
	if header == nil {
		return ""
	}
	if img := header.GetImageMessage(); img != nil {
		return img.GetCaption()
	}
	if vid := header.GetVideoMessage(); vid != nil {
		return vid.GetCaption()
	}
	if doc := header.GetDocumentMessage(); doc != nil {
		return doc.GetCaption()
	}
	return ""
}

// nativeFlowButtonParams covers the fields used by the button types rendered
// specially (cta_url, cta_call, cta_copy), plus the id and single_select rows
// that nativeFlowAsButton exposes to webhooks. Other button names (e.g.
// review_and_pay) fall through to displaying the raw button name, since their
// params carry structured payment data that doesn't reduce to a single line.
type nativeFlowButtonParams struct {
	DisplayText string `json:"display_text"`
	URL         string `json:"url"`
	PhoneNumber string `json:"phone_number"`
	Copy        string `json:"copy_code"`
	ID          string `json:"id"`
	Sections    []struct {
		Rows []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"rows"`
	} `json:"sections"`
	// Title is the visible label field used by picker-style buttons (e.g.
	// single_select's {"title":...,"sections":...}), which don't set
	// display_text at all.
	Title string `json:"title"`
}

func formatNativeFlowButton(button *waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton) string {
	if button == nil {
		return ""
	}
	name := button.GetName()

	var params nativeFlowButtonParams
	_ = json.Unmarshal([]byte(button.GetButtonParamsJSON()), &params)

	switch name {
	case "cta_url":
		label := params.DisplayText
		if label == "" {
			label = "Link"
		}
		if params.URL != "" {
			return fmt.Sprintf("🔗 %s: %s", label, params.URL)
		}
	case "cta_call":
		label := params.DisplayText
		if label == "" {
			label = "Call"
		}
		if params.PhoneNumber != "" {
			return fmt.Sprintf("📞 %s: %s", label, params.PhoneNumber)
		}
	case "cta_copy":
		label := params.DisplayText
		if label == "" {
			label = "Copy code"
		}
		if params.Copy != "" {
			return fmt.Sprintf("📋 %s: %s", label, params.Copy)
		}
	}

	label := params.DisplayText
	if label == "" {
		label = params.Title
	}
	if label != "" {
		return fmt.Sprintf("[%s] %s", name, label)
	}
	return fmt.Sprintf("[%s]", name)
}
