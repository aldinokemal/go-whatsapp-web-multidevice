package controllers

import (
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/services"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
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
}

func (controller *SendController) SendText(c *fiber.Ctx) error {
	var request structs.SendMessageRequest
	err := c.BodyParser(&request)
	utils.PanicIfNeeded(err)

	fmt.Println(request)
	request.PhoneNumber = request.PhoneNumber + "@s.whatsapp.net"
	response, err := controller.Service.SendText(c, request)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success",
		Results: response,
	})
}
