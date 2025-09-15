package rest

import (
	"fmt"
	"time"

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
	app.Get("/app/health", rest.HealthCheck)

	return App{Service: service}
}

func (handler *App) Login(c *fiber.Ctx) error {
	response, err := handler.Service.Login(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Login success",
		Results: map[string]any{
			"qr_link":     fmt.Sprintf("%s://%s%s/%s", c.Protocol(), c.Hostname(), config.AppBasePath, response.ImagePath),
			"qr_duration": response.Duration,
		},
	})
}

func (handler *App) LoginWithCode(c *fiber.Ctx) error {
	pairCode, err := handler.Service.LoginWithCode(c.UserContext(), c.Query("phone"))
	utils.PanicIfNeeded(err)

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
	err := handler.Service.Logout(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success logout",
		Results: nil,
	})
}

func (handler *App) Reconnect(c *fiber.Ctx) error {
	err := handler.Service.Reconnect(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Reconnect success",
		Results: nil,
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
	isConnected, isLoggedIn, deviceID := whatsapp.GetConnectionStatus()

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Connection status retrieved",
		Results: map[string]any{
			"is_connected": isConnected,
			"is_logged_in": isLoggedIn,
			"device_id":    deviceID,
		},
	})
}

func (handler *App) HealthCheck(c *fiber.Ctx) error {
	isConnected, isLoggedIn, deviceID := whatsapp.GetConnectionStatus()

	// Determine health status
	isHealthy := isConnected && isLoggedIn

	// Build detailed status information
	status := "healthy"
	if !isHealthy {
		status = "unhealthy"
	}

	// Detailed status message for debugging
	var statusMessage string
	if !isConnected && !isLoggedIn {
		statusMessage = "WhatsApp client is disconnected and not logged in - requires login"
	} else if !isConnected {
		statusMessage = "WhatsApp client is disconnected - attempting reconnection"
	} else if !isLoggedIn {
		statusMessage = "WhatsApp client is connected but not logged in - requires login"
	} else {
		statusMessage = "WhatsApp client is connected and logged in"
	}

	// Build response
	response := utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: statusMessage,
		Results: map[string]any{
			"status":       status,
			"is_connected": isConnected,
			"is_logged_in": isLoggedIn,
			"device_id":    deviceID,
			"timestamp":    time.Now().Unix(),
		},
	}

	// Set appropriate HTTP status code for monitoring tools
	if !isHealthy {
		// Return 503 Service Unavailable for unhealthy status
		// This helps monitoring tools like Uptime Kuma detect issues
		c.Status(503)
		response.Status = 503
		response.Code = "SERVICE_UNAVAILABLE"
	}

	return c.JSON(response)
}
