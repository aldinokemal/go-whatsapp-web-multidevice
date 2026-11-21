package rest

import (
	"fmt"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sysmetrics"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
	"go.mau.fi/whatsmeow/types"
)

type App struct {
	Service domainApp.IAppUsecase
}

func InitRestApp(app fiber.Router, service domainApp.IAppUsecase) App {
	rest := App{Service: service}
	app.Get("/app/login", rest.Login)
	app.Get("/app/login-with-code", rest.LoginWithCode)
	app.Get("/app/passkey", rest.PasskeyChallenge)
	app.Post("/app/passkey/response", rest.PasskeyResponse)
	app.Post("/app/passkey/confirm", rest.PasskeyConfirm)
	app.Get("/app/logout", rest.Logout)
	app.Get("/app/reconnect", rest.Reconnect)
	app.Get("/app/devices", rest.Devices)
	app.Get("/app/status", rest.ConnectionStatus)

	return App{Service: service}
}

// InitRestAppInfo registers /app/info outside the device-scoped group so
// standalone UIs can read server metadata before any device exists.
func InitRestAppInfo(app fiber.Router) {
	app.Get("/app/info", AppInfo)
	app.Get("/app/metrics", AppMetrics)
}

// AppMetrics reports the health of the instance itself — memory of the process and of
// its container, disk used by media and storages, free space on the volume, and how
// many devices are registered, logged in and connected — so a panel can show whether
// the server needs attention before a phone stops receiving messages.
func AppMetrics(c fiber.Ctx) error {
	snapshot := sysmetrics.Collect(map[string]string{
		"media":    config.PathMedia,
		"storages": config.PathStorages,
	}, config.PathStorages)

	devices := map[string]int{"total": 0, "logged_in": 0, "connected": 0}
	if dm := whatsapp.GetDeviceManager(); dm != nil {
		for _, device := range dm.ListDevices() {
			devices["total"]++
			if device.IsLoggedIn() {
				devices["logged_in"]++
			}
			if device.IsConnected() {
				devices["connected"]++
			}
		}
	}

	sessionStore := "sqlite"
	if strings.HasPrefix(strings.Trim(config.DBURI, "\"'"), "postgres:") {
		sessionStore = "postgres"
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Fetch app metrics success",
		Results: map[string]any{
			"version":       config.AppVersion,
			"session_store": sessionStore,
			"devices":       devices,
			"system":        snapshot,
		},
	})
}

func AppInfo(c fiber.Ctx) error {
	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Fetch app info success",
		Results: map[string]any{
			"version":          config.AppVersion,
			"os":               config.AppOs,
			"base_path":        config.AppBasePath,
			"max_file_size":    config.WhatsappSettingMaxFileSize,
			"max_video_size":   config.WhatsappSettingMaxVideoSize,
			"max_image_size":   config.WhatsappSettingMaxImageSize,
			"chatwoot_enabled": config.ChatwootEnabled,
			"scheduled_sends":  true,
		},
	})
}

func (handler *App) Login(c fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	response, err := handler.Service.Login(c.Context(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Login success",
		Results: map[string]any{
			"device_id":   device.ID(),
			"qr_link":     fmt.Sprintf("%s://%s%s/%s", c.Scheme(), c.Host(), config.AppBasePath, response.ImagePath),
			"qr_duration": response.Duration,
		},
	})
}

func (handler *App) LoginWithCode(c fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	pairCode, err := handler.Service.LoginWithCode(c.Context(), device.ID(), c.Query("phone"))
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

func (handler *App) PasskeyChallenge(c fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	response, err := handler.Service.PasskeyChallenge(c.Context(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Passkey pairing status",
		Results: map[string]any{
			"device_id":       device.ID(),
			"status":          response.Status,
			"challenge":       response.Challenge,
			"code":            response.Code,
			"skip_handoff_ux": response.SkipHandoffUX,
		},
	})
}

func (handler *App) PasskeyResponse(c fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	var assertion types.WebAuthnResponse
	if err := c.Bind().Body(&assertion); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid WebAuthn assertion payload: "+err.Error())
	}

	err = handler.Service.PasskeyResponse(c.Context(), device.ID(), &assertion)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Passkey response sent",
		Results: map[string]any{"device_id": device.ID()},
	})
}

func (handler *App) PasskeyConfirm(c fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	err = handler.Service.PasskeyConfirm(c.Context(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Passkey pairing confirmed",
		Results: map[string]any{"device_id": device.ID()},
	})
}

func (handler *App) Logout(c fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	err = handler.Service.Logout(c.Context(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Success logout",
		Results: map[string]any{"device_id": device.ID()},
	})
}

func (handler *App) Reconnect(c fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	err = handler.Service.Reconnect(c.Context(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Reconnect success",
		Results: map[string]any{"device_id": device.ID()},
	})
}

func (handler *App) Devices(c fiber.Ctx) error {
	devices, err := handler.Service.FetchDevices(c.Context())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Fetch device success",
		Results: devices,
	})
}

func (handler *App) ConnectionStatus(c fiber.Ctx) error {
	device, err := getDeviceInstance(c)
	if err != nil {
		return err
	}

	isConnected, isLoggedIn, err := handler.Service.Status(c.Context(), device.ID())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Connection status retrieved",
		Results: map[string]any{
			"is_connected": isConnected,
			"is_logged_in": isLoggedIn,
			"device_id":    device.ID(),
			"jid":          device.JID(),
		},
	})
}

func getDeviceInstance(c fiber.Ctx) (*whatsapp.DeviceInstance, error) {
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
