package rest

import (
	"fmt"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/gofiber/fiber/v2"
)

type App struct {
	Service domainApp.IAppService
}

func InitRestApp(app *fiber.App, service domainApp.IAppService) App {
	rest := App{Service: service}
	app.Get("/app/login", rest.Login)
	app.Get("/app/logout", rest.Logout)
	app.Get("/app/reconnect", rest.Reconnect)
	app.Get("/app/devices", rest.Devices)

	return App{Service: service}
}

func (controller *App) Login(c *fiber.Ctx) error {
	response, err := controller.Service.Login(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success",
		Results: map[string]any{
			"qr_link":     fmt.Sprintf("%s://%s/%s", c.Protocol(), c.Hostname(), response.ImagePath),
			"qr_duration": response.Duration,
		},
	})
}

func (controller *App) Logout(c *fiber.Ctx) error {
	err := controller.Service.Logout(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Success logout",
		Results: nil,
	})
}

func (controller *App) Reconnect(c *fiber.Ctx) error {
	err := controller.Service.Reconnect(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Reconnect success",
		Results: nil,
	})
}

func (controller *App) Devices(c *fiber.Ctx) error {
	devices, err := controller.Service.FetchDevices(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Code:    200,
		Message: "Fetch device success",
		Results: devices,
	})
}
