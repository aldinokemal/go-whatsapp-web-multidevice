package rest

import (
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type Message struct {
	Service domainMessage.IMessageUsecase
}

func InitRestMessage(app fiber.Router, service domainMessage.IMessageUsecase) Message {
	rest := Message{Service: service}

	// Message action endpoints
	app.Post("/message/:message_id/reaction", rest.ReactMessage)
	app.Post("/message/:message_id/revoke", rest.RevokeMessage)
	app.Post("/message/:message_id/delete", rest.DeleteMessage)
	app.Post("/message/:message_id/update", rest.UpdateMessage)
	app.Post("/message/:message_id/read", rest.MarkAsRead)
	app.Post("/message/:message_id/star", rest.StarMessage)
	app.Post("/message/:message_id/unstar", rest.UnstarMessage)
	app.Get("/message/:message_id/download", rest.DownloadMedia)
	return rest
}

func (controller *Message) RevokeMessage(c *fiber.Ctx) error {
	var request domainMessage.RevokeRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.RevokeMessage(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) DeleteMessage(c *fiber.Ctx) error {
	var request domainMessage.DeleteRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	err = controller.Service.DeleteMessage(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Message deleted successfully",
		Results: nil,
	})
}

func (controller *Message) UpdateMessage(c *fiber.Ctx) error {
	var request domainMessage.UpdateMessageRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.UpdateMessage(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) ReactMessage(c *fiber.Ctx) error {
	var request domainMessage.ReactionRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.ReactMessage(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) MarkAsRead(c *fiber.Ctx) error {
	var request domainMessage.MarkAsReadRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.MarkAsRead(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}

func (controller *Message) StarMessage(c *fiber.Ctx) error {
	var request domainMessage.StarRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)
	request.IsStarred = true

	err = controller.Service.StarMessage(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Starred message successfully",
		Results: nil,
	})
}

func (controller *Message) UnstarMessage(c *fiber.Ctx) error {
	var request domainMessage.StarRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	request.MessageID = c.Params("message_id")
	utils.SanitizePhone(&request.Phone)
	request.IsStarred = false
	err = controller.Service.StarMessage(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Unstarred message successfully",
		Results: nil,
	})
}

func (controller *Message) DownloadMedia(c *fiber.Ctx) error {
	var request domainMessage.DownloadMediaRequest

	request.MessageID = c.Params("message_id")
	request.Phone = c.Query("phone")
	utils.SanitizePhone(&request.Phone)

	response, err := controller.Service.DownloadMedia(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: response.Status,
		Results: response,
	})
}
