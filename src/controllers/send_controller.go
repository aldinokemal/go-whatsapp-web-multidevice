package controllers

import (
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/gofiber/fiber/v2"
)

type SendController struct {
	Service domainSend.ISendService
}

func NewSendController(service domainSend.ISendService) SendController {
	return SendController{Service: service}
}

func (controller *SendController) Route(app *fiber.App) {
	app.Post("/send/message", controller.SendText)
	app.Post("/send/image", controller.SendImage)
	app.Post("/send/file", controller.SendFile)
	app.Post("/send/video", controller.SendVideo)
	app.Post("/send/contact", controller.SendContact)
}

func (controller *SendController) SendText(c *fiber.Ctx) error {
	var request domainSend.MessageRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send message
	validations.ValidateSendMessage(request)

	if request.Type == domainSend.TypeGroup {
		request.Phone = request.Phone + "@g.us"
	} else {
		request.Phone = request.Phone + "@s.whatsapp.net"
	}

	response, err := controller.Service.SendText(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *SendController) SendImage(c *fiber.Ctx) error {
	var request domainSend.ImageRequest
	request.Compress = true

	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	file, err := c.FormFile("image")
	utils.PanicIfNeeded(err)

	request.Image = file

	//add validation send image
	validations.ValidateSendImage(request)

	if request.Type == domainSend.TypeGroup {
		request.Phone = request.Phone + "@g.us"
	} else {
		request.Phone = request.Phone + "@s.whatsapp.net"
	}

	response, err := controller.Service.SendImage(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *SendController) SendFile(c *fiber.Ctx) error {
	var request domainSend.FileRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	file, err := c.FormFile("file")
	utils.PanicIfNeeded(err)

	request.File = file

	//add validation send image
	validations.ValidateSendFile(request)

	if request.Type == domainSend.TypeGroup {
		request.Phone = request.Phone + "@g.us"
	} else {
		request.Phone = request.Phone + "@s.whatsapp.net"
	}

	response, err := controller.Service.SendFile(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *SendController) SendVideo(c *fiber.Ctx) error {
	var request domainSend.VideoRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	video, err := c.FormFile("video")
	utils.PanicIfNeeded(err)

	request.Video = video

	//add validation send image
	validations.ValidateSendVideo(request)

	if request.Type == domainSend.TypeGroup {
		request.Phone = request.Phone + "@g.us"
	} else {
		request.Phone = request.Phone + "@s.whatsapp.net"
	}

	response, err := controller.Service.SendVideo(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}

func (controller *SendController) SendContact(c *fiber.Ctx) error {
	var request domainSend.ContactRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	// add validation send contect
	validations.ValidateSendContact(request)

	if request.Type == domainSend.TypeGroup {
		request.Phone = request.Phone + "@g.us"
	} else {
		request.Phone = request.Phone + "@s.whatsapp.net"
	}

	response, err := controller.Service.SendContact(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: response.Status,
		Results: response,
	})
}
