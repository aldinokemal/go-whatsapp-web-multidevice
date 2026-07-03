package rest

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
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

	return rest
}

func (handler *Device) ListDevices(c *fiber.Ctx) error {
	devices, err := handler.Service.ListDevices(c.UserContext())
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "List devices",
		Results: devices,
	})
}

func (handler *Device) GetDevice(c *fiber.Ctx) error {
	deviceID := c.Params("device_id")
	device, err := handler.Service.GetDevice(c.UserContext(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device info",
		Results: device,
	})
}

func (handler *Device) AddDevice(c *fiber.Ctx) error {
	var req struct {
		DeviceID                  string `json:"device_id"`
		WebhookURL                string `json:"webhook_url"`
		WebhookSecret             string `json:"webhook_secret"`
		WebhookEvents             string `json:"webhook_events"`
		WebhookInsecureSkipVerify bool   `json:"webhook_insecure_skip_verify"`
	}

	if err := c.BodyParser(&req); err != nil {
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

	device, err := handler.Service.AddDevice(c.UserContext(), req.DeviceID, webhook)
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

func (handler *Device) RemoveDevice(c *fiber.Ctx) error {
	deviceID := c.Params("device_id")
	err := handler.Service.RemoveDevice(c.UserContext(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Device removed",
		Results: nil,
	})
}

func (handler *Device) LoginDevice(c *fiber.Ctx) error {
	deviceID := c.Params("device_id")
	err := handler.Service.LoginDevice(c.UserContext(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Login started",
		Results: map[string]any{"device_id": deviceID},
	})
}

func (handler *Device) LoginDeviceWithCode(c *fiber.Ctx) error {
	deviceID := c.Params("device_id")
	code, err := handler.Service.LoginDeviceWithCode(c.UserContext(), deviceID, c.Query("phone"))
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

func (handler *Device) LogoutDevice(c *fiber.Ctx) error {
	deviceID := c.Params("device_id")
	err := handler.Service.LogoutDevice(c.UserContext(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Logout requested",
		Results: nil,
	})
}

func (handler *Device) ReconnectDevice(c *fiber.Ctx) error {
	deviceID := c.Params("device_id")
	err := handler.Service.ReconnectDevice(c.UserContext(), deviceID)
	utils.PanicIfNeeded(err)

	return c.JSON(utils.ResponseData{
		Status:  200,
		Code:    "SUCCESS",
		Message: "Reconnect requested",
		Results: nil,
	})
}

func (handler *Device) Status(c *fiber.Ctx) error {
	deviceID := c.Params("device_id")
	isConnected, isLoggedIn, err := handler.Service.GetStatus(c.UserContext(), deviceID)
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
func (handler *Device) UpdateDeviceWebhook(c *fiber.Ctx) error {
	deviceID := c.Params("device_id")
	var req struct {
		WebhookURL                *string `json:"webhook_url"`
		WebhookSecret             string  `json:"webhook_secret"`
		WebhookEvents             string  `json:"webhook_events"`
		WebhookInsecureSkipVerify bool    `json:"webhook_insecure_skip_verify"`
	}

	if err := c.BodyParser(&req); err != nil {
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

	err := handler.Service.SetDeviceWebhookConfig(c.UserContext(), deviceID, config)
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
func (handler *Device) GetDeviceWebhook(c *fiber.Ctx) error {
	deviceID := c.Params("device_id")
	config, err := handler.Service.GetDeviceWebhookConfig(c.UserContext(), deviceID)
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
