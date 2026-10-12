package usecase

import (
	"encoding/json"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestBuildButtonReplyMessage(t *testing.T) {
	quote := &waE2E.ContextInfo{StanzaID: proto.String("3EB0ORIGINAL")}
	request := func(kind string) domainSend.ButtonReplyRequest {
		return domainSend.ButtonReplyRequest{Kind: kind, SelectedID: "opt-1", Text: "Quero saber mais"}
	}

	msg, err := BuildButtonReplyMessage(request("buttons"), quote)
	if err != nil {
		t.Fatal(err)
	}
	if r := msg.GetButtonsResponseMessage(); r.GetSelectedButtonID() != "opt-1" || r.GetSelectedDisplayText() != "Quero saber mais" ||
		r.GetType() != waE2E.ButtonsResponseMessage_DISPLAY_TEXT || r.GetContextInfo().GetStanzaID() != "3EB0ORIGINAL" {
		t.Fatalf("buttons reply: %+v", r)
	}

	index := uint32(2)
	tpl := request("template")
	tpl.SelectedIndex = &index
	msg, err = BuildButtonReplyMessage(tpl, quote)
	if err != nil {
		t.Fatal(err)
	}
	if r := msg.GetTemplateButtonReplyMessage(); r.GetSelectedID() != "opt-1" || r.GetSelectedIndex() != 2 || r.GetContextInfo() == nil {
		t.Fatalf("template reply: %+v", r)
	}

	list := request("list")
	list.Description = "Plano anual"
	msg, err = BuildButtonReplyMessage(list, quote)
	if err != nil {
		t.Fatal(err)
	}
	if r := msg.GetListResponseMessage(); r.GetSingleSelectReply().GetSelectedRowID() != "opt-1" || r.GetTitle() != "Quero saber mais" ||
		r.GetDescription() != "Plano anual" || r.GetListType() != waE2E.ListResponseMessage_SINGLE_SELECT {
		t.Fatalf("list reply: %+v", r)
	}

	msg, err = BuildButtonReplyMessage(request("interactive"), quote)
	if err != nil {
		t.Fatal(err)
	}
	flow := msg.GetInteractiveResponseMessage().GetNativeFlowResponseMessage()
	var params map[string]string
	_ = json.Unmarshal([]byte(flow.GetParamsJSON()), &params)
	if flow.GetName() != "quick_reply" || params["id"] != "opt-1" || msg.GetInteractiveResponseMessage().GetBody().GetText() != "Quero saber mais" {
		t.Fatalf("interactive reply: %+v", msg.GetInteractiveResponseMessage())
	}

	// The text falls back to the id, so the chat never shows an empty bubble.
	noText := request("buttons")
	noText.Text = ""
	msg, _ = BuildButtonReplyMessage(noText, quote)
	if msg.GetButtonsResponseMessage().GetSelectedDisplayText() != "opt-1" {
		t.Fatal("empty text should fall back to the id")
	}

	if _, err := BuildButtonReplyMessage(domainSend.ButtonReplyRequest{Kind: "buttons"}, quote); err == nil {
		t.Fatal("missing selected_id must be refused")
	}
	if _, err := BuildButtonReplyMessage(request("carousel"), quote); err == nil {
		t.Fatal("unknown kind must be refused")
	}
}
