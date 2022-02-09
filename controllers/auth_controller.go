package controllers

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/services"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/gofiber/fiber/v2"
)

type AuthController struct {
	Service services.AuthService
}

func NewAuthController(service services.AuthService) AuthController {
	return AuthController{Service: service}
}

func (controller *AuthController) Route(app *fiber.App) {
	app.Get("/auth/login", controller.Login)
	app.Get("/auth/logout", controller.Logout)
}

func (controller *AuthController) Login(c *fiber.Ctx) error {
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

func (controller *AuthController) Logout(c *fiber.Ctx) error {
	err := controller.Service.Logout(c)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success logout",
		Results: nil,
	})
}
