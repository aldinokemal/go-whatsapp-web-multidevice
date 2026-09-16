package rest

import (
	"encoding/json"
	"fmt"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
)

type Device struct {
	Service device.IDeviceUsecase
}

func InitRestDevice(app fiber.Router, service device.IDeviceUsecase) Device {
	rest := Device{Service: service}

	app.Get("/devices", rest.ListDevices)
	app.Post("/devices", rest.AddDevice)

	app.Get("/devices/:device_id", rest.GetDevice)
	app.Delete("/devices/:device_id", rest.RemoveDevice)

	app.Get("/devices/:device_id/login", rest.LoginDevice)
	app.Post("/devices/:device_id/login/code", rest.LoginDeviceWithCode)
	app.Post("/devices/:device_id/logout", rest.LogoutDevice)
	app.Post("/devices/:device_id/reconnect", rest.ReconnectDevice)
	app.Get("/devices/:device_id/status", rest.Status)
	app.Patch("/devices/:device_id/webhook", rest.UpdateDeviceWebhook)
	app.Get("/devices/:device_id/webhook", rest.GetDeviceWebhook)
	app.Patch("/devices/:device_id/settings", rest.UpdateDeviceStorageSettings)
	app.Get("/devices/:device_id/settings", rest.GetDeviceStorageSettings)

	return rest
}

func (handler *Device) ListDevices(c fiber.Ctx) error {
	devices, err := handler.Service.ListDevices(c.Context())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "List devices",
		Results: devices,
	})
}

func (handler *Device) GetDevice(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	device, err := handler.Service.GetDevice(c.Context(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device info",
		Results: device,
	})
}

func (handler *Device) AddDevice(c fiber.Ctx) error {
	var req struct {
		DeviceID                  string `json:"device_id"`
		WebhookURL                string `json:"webhook_url"`
		WebhookSecret             string `json:"webhook_secret"`
		WebhookEvents             string `json:"webhook_events"`
		WebhookInsecureSkipVerify bool   `json:"webhook_insecure_skip_verify"`
	}

	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}

	var webhook *chatstorage.DeviceWebhookConfig
	if req.WebhookURL != "" || req.WebhookSecret != "" || req.WebhookEvents != "" || req.WebhookInsecureSkipVerify {
		webhook = &chatstorage.DeviceWebhookConfig{
			WebhookURL:                &req.WebhookURL,
			WebhookSecret:             req.WebhookSecret,
			WebhookEvents:             req.WebhookEvents,
			WebhookInsecureSkipVerify: req.WebhookInsecureSkipVerify,
		}
	}

	device, err := handler.Service.AddDevice(c.Context(), req.DeviceID, webhook)
	utils.PanicIfNeeded(err)

	result := map[string]any{
		"id":           device.ID,
		"display_name": device.DisplayName,
		"jid":          device.JID,
		"state":        device.State,
		"created_at":   device.CreatedAt,
	}
	if webhook != nil {
		result["webhook_url"] = req.WebhookURL
		result["webhook_secret"] = req.WebhookSecret
		result["webhook_events"] = req.WebhookEvents
		result["webhook_insecure_skip_verify"] = req.WebhookInsecureSkipVerify
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device added",
		Results: result,
	})
}

func (handler *Device) RemoveDevice(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	err := handler.Service.RemoveDevice(c.Context(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device removed",
		Results: nil,
	})
}

func (handler *Device) LoginDevice(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	response, err := handler.Service.LoginDevice(c.Context(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Login success",
		Results: map[string]any{
			"device_id":   deviceID,
			"qr_link":     fmt.Sprintf("%s://%s%s/%s", c.Scheme(), c.Host(), config.AppBasePath, response.ImagePath),
			"qr_duration": response.Duration,
		},
	})
}

func (handler *Device) LoginDeviceWithCode(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	code, err := handler.Service.LoginDeviceWithCode(c.Context(), deviceID, c.Query("phone"))
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Login with code started",
		Results: map[string]any{
			"device_id": deviceID,
			"pair_code": code,
		},
	})
}

func (handler *Device) LogoutDevice(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	err := handler.Service.LogoutDevice(c.Context(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Logout requested",
		Results: nil,
	})
}

func (handler *Device) ReconnectDevice(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	err := handler.Service.ReconnectDevice(c.Context(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Reconnect requested",
		Results: nil,
	})
}

func (handler *Device) Status(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	isConnected, isLoggedIn, err := handler.Service.GetStatus(c.Context(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device status",
		Results: map[string]any{
			"device_id":    deviceID,
			"is_connected": isConnected,
			"is_logged_in": isLoggedIn,
		},
	})
}

// UpdateDeviceWebhook handles PATCH /devices/:device_id/webhook.
func (handler *Device) UpdateDeviceWebhook(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	var req struct {
		WebhookURL                *string `json:"webhook_url"`
		WebhookSecret             string  `json:"webhook_secret"`
		WebhookEvents             string  `json:"webhook_events"`
		WebhookInsecureSkipVerify bool    `json:"webhook_insecure_skip_verify"`
	}

	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}

	if req.WebhookURL == nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "webhook_url is required",
			Results: nil,
		})
	}

	config := &chatstorage.DeviceWebhookConfig{
		WebhookURL:                req.WebhookURL,
		WebhookSecret:             req.WebhookSecret,
		WebhookEvents:             req.WebhookEvents,
		WebhookInsecureSkipVerify: req.WebhookInsecureSkipVerify,
	}

	err := handler.Service.SetDeviceWebhookConfig(c.Context(), deviceID, config)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device webhook updated",
		Results: map[string]any{
			"device_id":                    deviceID,
			"webhook_url":                  *req.WebhookURL,
			"webhook_secret":               req.WebhookSecret,
			"webhook_events":               req.WebhookEvents,
			"webhook_insecure_skip_verify": req.WebhookInsecureSkipVerify,
		},
	})
}

// GetDeviceWebhook handles GET /devices/:device_id/webhook.
func (handler *Device) GetDeviceWebhook(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	config, err := handler.Service.GetDeviceWebhookConfig(c.Context(), deviceID)
	utils.PanicIfNeeded(err)

	webhookURL := ""
	if config != nil && config.WebhookURL != nil {
		webhookURL = *config.WebhookURL
	}

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device webhook retrieved",
		Results: map[string]any{
			"device_id":   deviceID,
			"webhook_url": webhookURL,
			"webhook_secret": func() string {
				if config != nil {
					return config.WebhookSecret
				}
				return ""
			}(),
			"webhook_events": func() string {
				if config != nil {
					return config.WebhookEvents
				}
				return ""
			}(),
			"webhook_insecure_skip_verify": func() bool {
				if config != nil {
					return config.WebhookInsecureSkipVerify
				}
				return false
			}(),
		},
	})
}

// UpdateDeviceStorageSettings handles PATCH /devices/:device_id/settings.
//
// The body is parsed as a raw JSON object (rather than a struct of *bool fields)
// so a field that is absent (leave the stored override untouched) can be told
// apart from a field explicitly sent as null (clear the override, follow the
// instance default): a struct field of type *bool decodes to nil in both cases.
func (handler *Device) UpdateDeviceStorageSettings(c fiber.Ctx) error {
	deviceID := c.Params("device_id")

	var raw map[string]json.RawMessage
	if err := c.Bind().Body(&raw); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "Invalid request body",
			Results: nil,
		})
	}

	var patch chatstorage.DeviceStoragePatch

	if rawValue, ok := raw["chat_storage"]; ok {
		var value *bool
		if err := json.Unmarshal(rawValue, &value); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
				Status:  400,
				Code:    "BAD_REQUEST",
				Message: "chat_storage must be a boolean or null",
				Results: nil,
			})
		}
		patch.HasChatStorage = true
		patch.ChatStorage = value
	}

	if rawValue, ok := raw["auto_download_media"]; ok {
		var value *bool
		if err := json.Unmarshal(rawValue, &value); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
				Status:  400,
				Code:    "BAD_REQUEST",
				Message: "auto_download_media must be a boolean or null",
				Results: nil,
			})
		}
		patch.HasAutoDownloadMedia = true
		patch.AutoDownloadMedia = value
	}

	if !patch.HasChatStorage && !patch.HasAutoDownloadMedia {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{
			Status:  400,
			Code:    "BAD_REQUEST",
			Message: "at least one of chat_storage or auto_download_media is required",
			Results: nil,
		})
	}

	err := handler.Service.SetDeviceStorageSettings(c.Context(), deviceID, patch)
	utils.PanicIfNeeded(err)

	settings, err := handler.Service.GetDeviceStorageSettings(c.Context(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device storage settings updated",
		Results: deviceStorageSettingsResult(deviceID, settings),
	})
}

// GetDeviceStorageSettings handles GET /devices/:device_id/settings.
func (handler *Device) GetDeviceStorageSettings(c fiber.Ctx) error {
	deviceID := c.Params("device_id")
	settings, err := handler.Service.GetDeviceStorageSettings(c.Context(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device storage settings retrieved",
		Results: deviceStorageSettingsResult(deviceID, settings),
	})
}

// deviceStorageSettingsResult builds the JSON-serializable response body shared
// by GET and PATCH /devices/:device_id/settings. A nil field means the device
// has no override and follows the instance-wide default.
func deviceStorageSettingsResult(deviceID string, settings *chatstorage.DeviceStorageSettings) map[string]any {
	var chatStorage, autoDownloadMedia any
	if settings != nil {
		if settings.ChatStorage != nil {
			chatStorage = *settings.ChatStorage
		}
		if settings.AutoDownloadMedia != nil {
			autoDownloadMedia = *settings.AutoDownloadMedia
		}
	}
	return map[string]any{
		"device_id":           deviceID,
		"chat_storage":        chatStorage,
		"auto_download_media": autoDownloadMedia,
	}
}
