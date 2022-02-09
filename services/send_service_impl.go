package services

import (
	"errors"
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/gofiber/fiber/v2"
	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

type SendServiceImpl struct {
	WaCli *whatsmeow.Client
}

func NewSendService(waCli *whatsmeow.Client) SendService {
	return &SendServiceImpl{
		WaCli: waCli,
	}
}

func (service SendServiceImpl) SendText(c *fiber.Ctx, request structs.SendMessageRequest) (response structs.SendMessageResponse, err error) {
	msg := &waProto.Message{Conversation: proto.String(request.Message)}
	recipient, ok := utils.ParseJID(request.PhoneNumber)
	if !ok {
		return response, errors.New("invalid JID " + request.PhoneNumber)
	}
	ts, err := service.WaCli.SendMessage(recipient, "", msg)
	if err != nil {
		return response, err
	} else {
		response.Status = fmt.Sprintf("Message sent (server timestamp: %s)", ts)
	}
	return response, nil
}

func (service SendServiceImpl) SendImage(c *fiber.Ctx) {
	//TODO implement me
	panic("implement me")
}
