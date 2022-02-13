package controllers

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/services"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/gofiber/fiber/v2"
)

type SendController struct {
	Service services.SendService
}

func NewSendController(service services.SendService) SendController {
	return SendController{Service: service}
}

func (controller *SendController) Route(app *fiber.App) {
	app.Post("/send/message", controller.SendText)
	app.Post("/send/image", controller.SendImage)
	app.Post("/send/file", controller.SendFile)
}

func (controller *SendController) SendText(c *fiber.Ctx) error {
	var request structs.SendMessageRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send message
	validations.ValidateSendMessage(request)

	request.Phone = request.Phone + "@s.whatsapp.net"
	response, err := controller.Service.SendText(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *SendController) SendImage(c *fiber.Ctx) error {
	var request structs.SendImageRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	file, err := c.FormFile("image")
	utils.PanicIfNeeded(err)

	request.Image = file

	//add validation send image
	validations.ValidateSendImage(request)

	request.Phone = request.Phone + "@s.whatsapp.net"
	response, err := controller.Service.SendImage(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *SendController) SendFile(c *fiber.Ctx) error {
	var request structs.SendFileRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	file, err := c.FormFile("file")
	utils.PanicIfNeeded(err)

	request.File = file

	//add validation send image
	validations.ValidateSendFile(request)

	request.Phone = request.Phone + "@s.whatsapp.net"
	response, err := controller.Service.SendFile(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}
