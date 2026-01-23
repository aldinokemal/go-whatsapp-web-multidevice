package rest

import (
	"fmt"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type App struct {
	Service domainApp.IAppUsecase
}

func InitRestApp(app fiber.Router, service domainApp.IAppUsecase) App {
	rest := App{Service: service}
	app.Get("/app/login", rest.Login)
	app.Get("/app/login-with-code", rest.LoginWithCode)
	app.Get("/app/logout", rest.Logout)
	app.Get("/app/reconnect", rest.Reconnect)
	app.Get("/app/devices", rest.Devices)
	app.Get("/app/status", rest.ConnectionStatus)

	return App{Service: service}
}

func (handler *App) Login(c *fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	response, err := handler.Service.Login(c.UserContext(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Login success",
		Results: map[string]any{
			"device_id":   device.ID(),
			"qr_link":     fmt.Sprintf("%s://%s%s/%s", c.Protocol(), c.Hostname(), config.AppBasePath, response.ImagePath),
			"qr_duration": response.Duration,
		},
	})
}

func (handler *App) LoginWithCode(c *fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	pairCode, err := handler.Service.LoginWithCode(c.UserContext(), device.ID(), c.Query("phone"))
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Login with code success",
		Results: map[string]any{
			"device_id": device.ID(),
			"pair_code": pairCode,
		},
	})
}

func (handler *App) Logout(c *fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	err = handler.Service.Logout(c.UserContext(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success logout",
		Results: map[string]any{"device_id": device.ID()},
	})
}

func (handler *App) Reconnect(c *fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	err = handler.Service.Reconnect(c.UserContext(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Reconnect success",
		Results: map[string]any{"device_id": device.ID()},
	})
}

func (handler *App) Devices(c *fiber.Ctx) error {
	devices, err := handler.Service.FetchDevices(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Fetch device success",
		Results: devices,
	})
}

func (handler *App) ConnectionStatus(c *fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	isConnected, isLoggedIn, err := handler.Service.Status(c.UserContext(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Connection status retrieved",
		Results: map[string]any{
			"is_connected": isConnected,
			"is_logged_in": isLoggedIn,
			"device_id":    device.ID(),
		},
	})
}

func getDeviceInstance(c *fiber.Ctx) (*whatsapp.DeviceInstance, error) {
	value := c.Locals("device")
	if value == nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "device context is missing")
	}
	device, ok := value.(*whatsapp.DeviceInstance)
	if !ok || device == nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid device context")
	}
	return device, nil
}
