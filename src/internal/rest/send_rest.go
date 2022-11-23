package rest

import (
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/gofiber/fiber/v2"
)

type Send struct {
	Service domainSend.ISendService
}

func InitRestSend(app *fiber.App, service domainSend.ISendService) Send {
	rest := Send{Service: service}
	app.Post("/send/message", rest.SendText)
	app.Post("/send/image", rest.SendImage)
	app.Post("/send/file", rest.SendFile)
	app.Post("/send/video", rest.SendVideo)
	app.Post("/send/contact", rest.SendContact)
	app.Post("/send/link", rest.SendLink)
	app.Post("/message/:message_id/revoke", rest.RevokeMessage)
	app.Post("/message/:message_id/update", rest.UpdateMessage)
	return rest
}

func (controller *Send) SendText(c *fiber.Ctx) error {
	var request domainSend.MessageRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send message
	validations.ValidateSendMessage(request)

	response, err := controller.Service.SendText(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendImage(c *fiber.Ctx) error {
	var request domainSend.ImageRequest
	request.Compress = true

	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	file, err := c.FormFile("image")
	utils.PanicIfNeeded(err)

	request.Image = file

	//add validation send image
	validations.ValidateSendImage(request)

	response, err := controller.Service.SendImage(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendFile(c *fiber.Ctx) error {
	var request domainSend.FileRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	file, err := c.FormFile("file")
	utils.PanicIfNeeded(err)

	request.File = file

	//add validation send image
	validations.ValidateSendFile(request)

	response, err := controller.Service.SendFile(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendVideo(c *fiber.Ctx) error {
	var request domainSend.VideoRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	video, err := c.FormFile("video")
	utils.PanicIfNeeded(err)

	request.Video = video

	//add validation send image
	validations.ValidateSendVideo(request)

	response, err := controller.Service.SendVideo(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendContact(c *fiber.Ctx) error {
	var request domainSend.ContactRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send contect
	validations.ValidateSendContact(request)

	response, err := controller.Service.SendContact(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) SendLink(c *fiber.Ctx) error {
	var request domainSend.LinkRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	err = validations.ValidateSendLink(request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.SendLink(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) RevokeMessage(c *fiber.Ctx) error {
	var request domainSend.RevokeRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)
	request.MessageID = c.Params("message_id")

	err = validations.ValidateRevokeMessage(request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.Revoke(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *Send) UpdateMessage(c *fiber.Ctx) error {
	var request domainSend.UpdateMessageRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)
	request.MessageID = c.Params("message_id")

	err = validations.ValidateUpdateMessage(request)
	utils.PanicIfNeeded(err)

	response, err := controller.Service.UpdateMessage(c.UserContext(), request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}
