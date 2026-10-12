package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// BuildButtonReplyMessage turns a button tap into the response message the phone would
// send for that kind of button. contextInfo carries the quote of the original message.
func BuildButtonReplyMessage(request domainSend.ButtonReplyRequest, contextInfo *waE2E.ContextInfo) (*waE2E.Message, error) {
	id := strings.TrimSpace(request.SelectedID)
	text := strings.TrimSpace(request.Text)
	if id == "" {
		return nil, pkgError.ValidationError("selected_id is required")
	}
	if text == "" {
		text = id
	}

	switch request.Kind {
	case "buttons":
		return &waE2E.Message{ButtonsResponseMessage: &waE2E.ButtonsResponseMessage{
			SelectedButtonID: proto.String(id),
			Response:         &waE2E.ButtonsResponseMessage_SelectedDisplayText{SelectedDisplayText: text},
			Type:             waE2E.ButtonsResponseMessage_DISPLAY_TEXT.Enum(),
			ContextInfo:      contextInfo,
		}}, nil
	case "template":
		return &waE2E.Message{TemplateButtonReplyMessage: &waE2E.TemplateButtonReplyMessage{
			SelectedID:          proto.String(id),
			SelectedDisplayText: proto.String(text),
			SelectedIndex:       request.SelectedIndex,
			ContextInfo:         contextInfo,
		}}, nil
	case "list":
		reply := &waE2E.ListResponseMessage{
			Title:             proto.String(text),
			ListType:          waE2E.ListResponseMessage_SINGLE_SELECT.Enum(),
			SingleSelectReply: &waE2E.ListResponseMessage_SingleSelectReply{SelectedRowID: proto.String(id)},
			ContextInfo:       contextInfo,
		}
		if request.Description != "" {
			reply.Description = proto.String(request.Description)
		}
		return &waE2E.Message{ListResponseMessage: reply}, nil
	case "interactive":
		name := strings.TrimSpace(request.FlowName)
		if name == "" {
			name = "quick_reply"
		}
		params, err := json.Marshal(map[string]string{"id": id})
		if err != nil {
			return nil, err
		}
		return &waE2E.Message{InteractiveResponseMessage: &waE2E.InteractiveResponseMessage{
			Body: &waE2E.InteractiveResponseMessage_Body{
				Text:   proto.String(text),
				Format: waE2E.InteractiveResponseMessage_Body_DEFAULT.Enum(),
			},
			InteractiveResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage_{
				NativeFlowResponseMessage: &waE2E.InteractiveResponseMessage_NativeFlowResponseMessage{
					Name:       proto.String(name),
					ParamsJSON: proto.String(string(params)),
					Version:    proto.Int32(3),
				},
			},
			ContextInfo: contextInfo,
		}}, nil
	}
	return nil, pkgError.ValidationError(fmt.Sprintf("kind must be buttons, template, list or interactive, got %q", request.Kind))
}

// SendButtonReply sends the response to a tapped button, quoting the original message.
func (service serviceSend) SendButtonReply(ctx context.Context, request domainSend.ButtonReplyRequest) (response domainSend.GenericResponse, err error) {
	if strings.TrimSpace(request.ReplyMessageID) == "" {
		return response, pkgError.ValidationError("reply_message_id is required: a button reply quotes the message that carried the button")
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}
	recipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	replyID := request.ReplyMessageID
	contextInfo := service.mergeReplyContext(ctx, &waE2E.ContextInfo{}, &replyID)
	msg, err := BuildButtonReplyMessage(request, contextInfo)
	if err != nil {
		return response, err
	}

	sent, err := service.wrapSendMessage(ctx, client, recipient, msg, request.Text)
	if err != nil {
		return response, err
	}
	response.MessageID = sent.ID
	response.Status = fmt.Sprintf("Button reply sent to %s (server timestamp: %s)", request.Phone, sent.Timestamp.String())
	return response, nil
}
