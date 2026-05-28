package rest

import (
	"strconv"
	"strings"

	domainWebhook "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/webhook"
	webhookinfra "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/webhook"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

// webhookConfigRequest is the API request shape. It keeps the domain DTO clean
// while letting device_id be auto-resolved and Enabled default to true
// (nil = not provided).
type webhookConfigRequest struct {
	DeviceID   string            `json:"device_id"`
	WebhookURL string            `json:"webhook_url"`
	Secret     string            `json:"secret"`
	Events     []string          `json:"events"`
	Enabled    *bool             `json:"enabled"`
	Headers    map[string]string `json:"headers"`
}

func (r *webhookConfigRequest) toConfig() domainWebhook.DeviceWebhookConfig {
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	return domainWebhook.DeviceWebhookConfig{
		DeviceID:   strings.TrimSpace(r.DeviceID),
		WebhookURL: strings.TrimSpace(r.WebhookURL),
		Secret:     r.Secret,
		Events:     r.Events,
		Enabled:    enabled,
		Headers:    r.Headers,
	}
}

// WebhookConfigHandler exposes CRUD over per-device webhook configs. Every write
// refreshes the in-memory registry so changes take effect without a restart.
type WebhookConfigHandler struct {
	Repo          domainWebhook.IDeviceWebhookRepository
	Registry      *webhookinfra.WebhookRegistry
	DeviceManager *whatsapp.DeviceManager
}

func NewWebhookConfigHandler(
	repo domainWebhook.IDeviceWebhookRepository,
	registry *webhookinfra.WebhookRegistry,
	dm *whatsapp.DeviceManager,
) *WebhookConfigHandler {
	return &WebhookConfigHandler{Repo: repo, Registry: registry, DeviceManager: dm}
}

// List returns every stored webhook config.
func (h *WebhookConfigHandler) List(c *fiber.Ctx) error {
	configs, err := h.Repo.GetAll()
	if err != nil {
		return h.internalError(c, err)
	}
	return c.JSON(utils.ResponseData{
		Code:    "SUCCESS",
		Message: "Device webhook configs",
		Results: nonNilConfigs(configs),
	})
}

// GetByDevice returns every webhook config bound to a device JID (1:N).
func (h *WebhookConfigHandler) GetByDevice(c *fiber.Ctx) error {
	configs, err := h.Repo.GetByDeviceID(deviceIDParam(c))
	if err != nil {
		return h.internalError(c, err)
	}
	return c.JSON(utils.ResponseData{
		Code:    "SUCCESS",
		Message: "Device webhook configs",
		Results: nonNilConfigs(configs),
	})
}

// Create stores a new webhook config. device_id is optional: when omitted it
// auto-selects the only connected device.
func (h *WebhookConfigHandler) Create(c *fiber.Ctx) error {
	var req webhookConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ResponseError(c, "Invalid payload")
	}
	cfg := req.toConfig()

	if cfg.DeviceID == "" {
		deviceID, errData := h.autoResolveDeviceID()
		if errData != nil {
			return c.Status(errData.Status).JSON(errData)
		}
		cfg.DeviceID = deviceID
	}

	cfg.ID = 0 // force INSERT
	return h.save(c, &cfg, fiber.StatusCreated, "Device webhook config created")
}

// Update overwrites the webhook config identified by the :id path param.
func (h *WebhookConfigHandler) Update(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return utils.ResponseError(c, "id must be a positive integer")
	}
	var req webhookConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ResponseError(c, "Invalid payload")
	}
	cfg := req.toConfig()
	cfg.ID = id // URL path is authoritative
	return h.save(c, &cfg, fiber.StatusOK, "Device webhook config updated")
}

// Delete removes the webhook config identified by the :id path param.
func (h *WebhookConfigHandler) Delete(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return utils.ResponseError(c, "id must be a positive integer")
	}
	if err := h.Repo.Delete(id); err != nil {
		return h.internalError(c, err)
	}
	h.refresh()
	return c.JSON(utils.ResponseData{Code: "SUCCESS", Message: "Device webhook config deleted"})
}

// save validates, persists and refreshes the registry. Shared by Create/Update.
func (h *WebhookConfigHandler) save(c *fiber.Ctx, cfg *domainWebhook.DeviceWebhookConfig, status int, message string) error {
	if cfg.DeviceID == "" {
		return utils.ResponseError(c, "device_id is required")
	}
	if cfg.WebhookURL == "" {
		return utils.ResponseError(c, "webhook_url is required")
	}
	if err := h.Repo.Save(cfg); err != nil {
		return h.internalError(c, err)
	}
	h.refresh()
	return c.Status(status).JSON(utils.ResponseData{
		Code:    "SUCCESS",
		Message: message,
		Results: cfg,
	})
}

// autoResolveDeviceID returns the JID of the only connected device, or a
// describing error response when there are zero or multiple devices.
func (h *WebhookConfigHandler) autoResolveDeviceID() (string, *utils.ResponseData) {
	if h.DeviceManager == nil {
		return "", &utils.ResponseData{Status: fiber.StatusBadRequest, Code: "INVALID_REQUEST", Message: "device manager unavailable; pass device_id explicitly"}
	}
	devices := h.DeviceManager.ListDevices()
	switch len(devices) {
	case 0:
		return "", &utils.ResponseData{Status: fiber.StatusBadRequest, Code: "NO_DEVICE", Message: "No device available; log in a device first or pass device_id"}
	case 1:
		return deviceJID(devices[0]), nil
	default:
		available := make([]map[string]string, 0, len(devices))
		for _, d := range devices {
			available = append(available, map[string]string{"device_id": deviceJID(d), "name": d.DisplayName()})
		}
		return "", &utils.ResponseData{Status: fiber.StatusBadRequest, Code: "MULTIPLE_DEVICES", Message: "Multiple devices connected; specify device_id", Results: available}
	}
}

func (h *WebhookConfigHandler) refresh() {
	if h.Registry == nil {
		return
	}
	if err := h.Registry.Refresh(); err != nil {
		logrus.Warnf("Webhook: failed to refresh registry: %v", err)
	}
}

func (h *WebhookConfigHandler) internalError(c *fiber.Ctx, err error) error {
	logrus.Errorf("Webhook config: %v", err)
	return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
		Code:    "INTERNAL_ERROR",
		Message: "Internal server error",
	})
}

// nonNilConfigs guarantees a JSON array ([] not null) for empty results.
func nonNilConfigs(configs []*domainWebhook.DeviceWebhookConfig) []*domainWebhook.DeviceWebhookConfig {
	if configs == nil {
		return []*domainWebhook.DeviceWebhookConfig{}
	}
	return configs
}
