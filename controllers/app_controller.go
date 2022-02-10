package controllers

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/services"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/gofiber/fiber/v2"
)

type AppController struct {
	Service services.AppService
}

func NewAppController(service services.AppService) AppController {
	return AppController{Service: service}
}

func (controller *AppController) Route(app *fiber.App) {
	app.Get("/app/login", controller.Login)
	app.Get("/app/logout", controller.Logout)
	app.Get("/app/reconnect", controller.Reconnect)
}

func (controller *AppController) Login(c *fiber.Ctx) error {
	response, err := controller.Service.Login(c)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success",
		Results: map[string]interface{}{
			"qr_link":     "http://localhost:3000/" + response.ImagePath,
			"qr_duration": response.Duration,
		},
	})
}

func (controller *AppController) Logout(c *fiber.Ctx) error {
	err := controller.Service.Logout(c)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success logout",
		Results: nil,
	})
}

func (controller *AppController) Reconnect(c *fiber.Ctx) error {
	err := controller.Service.Reconnect(c)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Reconnect success",
		Results: nil,
	})
}
