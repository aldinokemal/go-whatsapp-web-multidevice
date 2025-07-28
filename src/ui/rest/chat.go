package rest

import (
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type Chat struct {
	Service domainChat.IChatUsecase
}

func InitRestChat(app fiber.Router, service domainChat.IChatUsecase) Chat {
	rest := Chat{Service: service}

	// Chat endpoints
	app.Get("/chats", rest.ListChats)
	app.Get("/chat/:chat_jid/messages", rest.GetChatMessages)
	app.Post("/chat/:chat_jid/pin", rest.PinChat)

	return rest
}

func (controller *Chat) ListChats(c *fiber.Ctx) error {
	var request domainChat.ListChatsRequest

	// Parse query parameters
	request.Limit = c.QueryInt("limit", 25)
	request.Offset = c.QueryInt("offset", 0)
	request.Search = c.Query("search", "")
	request.HasMedia = c.QueryBool("has_media", false)

	response, err := controller.Service.ListChats(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get chat list",
		Results: response,
	})
}

func (controller *Chat) GetChatMessages(c *fiber.Ctx) error {
	var request domainChat.GetChatMessagesRequest

	// Parse path parameter
	request.ChatJID = c.Params("chat_jid")

	// Parse query parameters
	request.Limit = c.QueryInt("limit", 50)
	request.Offset = c.QueryInt("offset", 0)
	request.MediaOnly = c.QueryBool("media_only", false)
	request.Search = c.Query("search", "")

	// Parse time filters
	if startTime := c.Query("start_time"); startTime != "" {
		request.StartTime = &startTime
	}
	if endTime := c.Query("end_time"); endTime != "" {
		request.EndTime = &endTime
	}

	// Parse is_from_me filter
	if isFromMeStr := c.Query("is_from_me"); isFromMeStr != "" {
		isFromMe := c.QueryBool("is_from_me")
		request.IsFromMe = &isFromMe
	}

	response, err := controller.Service.GetChatMessages(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get chat messages",
		Results: response,
	})
}

func (controller *Chat) PinChat(c *fiber.Ctx) error {
	var request domainChat.PinChatRequest

	// Parse path parameter
	request.ChatJID = c.Params("chat_jid")

	// Parse JSON body
	if err := c.BodyParser(&request); err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}

	response, err := controller.Service.PinChat(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Message,
		Results: response,
	})
}
