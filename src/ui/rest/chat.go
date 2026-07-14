package rest

import (
	"net/url"
	"strings"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
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
	app.Post("/chat/:chat_jid/disappearing", rest.SetDisappearingTimer)
	app.Post("/chat/:chat_jid/archive", rest.ArchiveChat)

	return rest
}

func (controller *Chat) ListChats(c fiber.Ctx) error {
	var request domainChat.ListChatsRequest

	// Parse query parameters
	request.Limit = fiber.Query[int](c, "limit", 25)
	request.Offset = fiber.Query[int](c, "offset", 0)
	request.Search = c.Query("search", "")
	request.HasMedia = fiber.Query[bool](c, "has_media", false)
	if archivedStr := c.Query("archived"); archivedStr != "" {
		isArchived := fiber.Query[bool](c, "archived")
		request.Archived = &isArchived
	}

	response, err := controller.Service.ListChats(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get chat list",
		Results: response,
	})
}

func (controller *Chat) GetChatMessages(c fiber.Ctx) error {
	var request domainChat.GetChatMessagesRequest

	// Parse path parameter
	chatJID, err := chatJIDParam(c)
	if err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "invalid chat_jid path parameter: " + err.Error(),
			Results: nil,
		})
	}
	request.ChatJID = chatJID

	// Parse query parameters
	request.Limit = fiber.Query[int](c, "limit", 50)
	request.Offset = fiber.Query[int](c, "offset", 0)
	request.MediaOnly = fiber.Query[bool](c, "media_only", false)
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
		isFromMe := fiber.Query[bool](c, "is_from_me")
		request.IsFromMe = &isFromMe
	}

	response, err := controller.Service.GetChatMessages(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success get chat messages",
		Results: response,
	})
}

func (controller *Chat) PinChat(c fiber.Ctx) error {
	var request domainChat.PinChatRequest

	// Parse path parameter
	chatJID, err := chatJIDParam(c)
	if err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "invalid chat_jid path parameter: " + err.Error(),
			Results: nil,
		})
	}
	request.ChatJID = chatJID

	// Parse JSON body
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}

	response, err := controller.Service.PinChat(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Message,
		Results: response,
	})
}

func (controller *Chat) SetDisappearingTimer(c fiber.Ctx) error {
	var request domainChat.SetDisappearingTimerRequest

	// Parse path parameter
	chatJID, err := chatJIDParam(c)
	if err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "invalid chat_jid path parameter: " + err.Error(),
			Results: nil,
		})
	}
	request.ChatJID = chatJID

	// Parse JSON body
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}

	response, err := controller.Service.SetDisappearingTimer(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Message,
		Results: response,
	})
}

func (controller *Chat) ArchiveChat(c fiber.Ctx) error {
	var request domainChat.ArchiveChatRequest

	// Parse path parameter
	chatJID, err := chatJIDParam(c)
	if err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "invalid chat_jid path parameter: " + err.Error(),
			Results: nil,
		})
	}
	request.ChatJID = chatJID

	// Parse JSON body
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(400).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}

	response, err := controller.Service.ArchiveChat(whatsapp.ContextWithDevice(c.Context(), getDeviceFromCtx(c)), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Message,
		Results: response,
	})
}

// chatJIDParam returns the chat_jid path parameter with percent-encoding
// decoded. Fiber does not unescape path params, so URL-encoding clients send
// "...%40g.us" which would miss every chat-storage lookup. strings.Clone
// detaches the no-escapes passthrough from fiber's reusable param buffer.
func chatJIDParam(c fiber.Ctx) (string, error) {
	decoded, err := url.PathUnescape(c.Params("chat_jid"))
	if err != nil {
		return "", err
	}
	return strings.Clone(decoded), nil
}
