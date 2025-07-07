package rest

import (
	"fmt"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type App struct {
	Service domainApp.IAppUsecase
}

func InitRestApp(app *fiber.App, service domainApp.IAppUsecase) App {
	rest := App{Service: service}
	app.Get("/app/login", rest.Login)
	app.Get("/app/login-with-code", rest.LoginWithCode)
	app.Get("/app/logout", rest.Logout)
	app.Get("/app/reconnect", rest.Reconnect)
	app.Get("/app/devices", rest.Devices)

	return App{Service: service}
}

func (handler *App) Login(c *fiber.Ctx) error {
	response, err := handler.Service.Login(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "LOGIN_FAILED",
			Message: fmt.Sprintf("Failed to login: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Login success",
		Results: map[string]any{
			"qr_link":     fmt.Sprintf("%s://%s/%s", c.Protocol(), c.Hostname(), response.ImagePath),
			"qr_duration": response.Duration,
		},
	})
}

func (handler *App) LoginWithCode(c *fiber.Ctx) error {
	phone := c.Query("phone")
	if phone == "" {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "PHONE_REQUIRED",
			Message: "Phone number is required",
		})
	}

	pairCode, err := handler.Service.LoginWithCode(c.UserContext(), phone)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "LOGIN_WITH_CODE_FAILED",
			Message: fmt.Sprintf("Failed to login with code: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Login with code success",
		Results: map[string]any{
			"pair_code": pairCode,
		},
	})
}

func (handler *App) Logout(c *fiber.Ctx) error {
	if err := handler.Service.Logout(c.UserContext()); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "LOGOUT_FAILED",
			Message: fmt.Sprintf("Failed to logout: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success logout",
		Results: nil,
	})
}

func (handler *App) Reconnect(c *fiber.Ctx) error {
	if err := handler.Service.Reconnect(c.UserContext()); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "RECONNECT_FAILED",
			Message: fmt.Sprintf("Failed to reconnect: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Reconnect success",
		Results: nil,
	})
}

func (handler *App) Devices(c *fiber.Ctx) error {
	devices, err := handler.Service.FetchDevices(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
			Status:  500,
			Code:    "FETCH_DEVICES_FAILED",
			Message: fmt.Sprintf("Failed to fetch devices: %v", err),
		})
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Fetch device success",
		Results: devices,
	})
}
