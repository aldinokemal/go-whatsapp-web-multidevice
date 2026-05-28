package rest

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatwoot "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

// chatwootConfigRequest is the API request shape. It keeps the domain DTO clean
// while allowing optional fields (device_id, inbox_id) to be auto-resolved and
// Enabled to default to true (nil = not provided).
type chatwootConfigRequest struct {
	DeviceID       string `json:"device_id"`
	ChatwootURL    string `json:"chatwoot_url"`
	APIToken       string `json:"api_token"`
	AccountID      int    `json:"account_id"`
	InboxID        int    `json:"inbox_id"`
	InboxName      string `json:"inbox_name"`
	Enabled        *bool  `json:"enabled"`
	ImportMessages bool   `json:"import_messages"`
	DaysLimit      int    `json:"days_limit"`
}

func (r *chatwootConfigRequest) toConfig() domainChatwoot.DeviceConfig {
	enabled := true
	if r.Enabled != nil {
		enabled = *r.Enabled
	}
	daysLimit := r.DaysLimit
	if daysLimit == 0 {
		daysLimit = 3
	}
	return domainChatwoot.DeviceConfig{
		DeviceID:       strings.TrimSpace(r.DeviceID),
		ChatwootURL:    strings.TrimSpace(r.ChatwootURL),
		APIToken:       strings.TrimSpace(r.APIToken),
		AccountID:      r.AccountID,
		InboxID:        r.InboxID,
		Enabled:        enabled,
		ImportMessages: r.ImportMessages,
		DaysLimit:      daysLimit,
	}
}

// chatwootConfigResponse augments a stored config with the (derived) webhook
// callback URL so callers can verify what GoWA registers on the Chatwoot inbox.
type chatwootConfigResponse struct {
	*domainChatwoot.DeviceConfig
	WebhookURL string `json:"webhook_url"`
}

// ChatwootConfigHandler exposes CRUD over per-device Chatwoot mappings. Every
// write refreshes the in-memory client registry so changes take effect without
// a restart.
type ChatwootConfigHandler struct {
	Repo          domainChatwoot.IDeviceConfigRepository
	Registry      *chatwoot.ClientRegistry
	DeviceManager *whatsapp.DeviceManager
}

func NewChatwootConfigHandler(
	repo domainChatwoot.IDeviceConfigRepository,
	registry *chatwoot.ClientRegistry,
	dm *whatsapp.DeviceManager,
) *ChatwootConfigHandler {
	return &ChatwootConfigHandler{Repo: repo, Registry: registry, DeviceManager: dm}
}

// List returns every stored mapping.
func (h *ChatwootConfigHandler) List(c *fiber.Ctx) error {
	configs, err := h.Repo.GetAll()
	if err != nil {
		return h.internalError(c, err)
	}
	results := make([]chatwootConfigResponse, 0, len(configs))
	for _, cfg := range configs {
		results = append(results, withWebhookURL(cfg))
	}
	return c.JSON(utils.ResponseData{
		Code:    "SUCCESS",
		Message: "Chatwoot device configs",
		Results: results,
	})
}

// deviceIDParam returns the :device_id path param, URL-decoded so encoded JIDs
// (e.g. "628xxx%40s.whatsapp.net") resolve to their real form. Fiber does not
// unescape path params by default.
func deviceIDParam(c *fiber.Ctx) string {
	raw := c.Params("device_id")
	if decoded, err := url.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

// Get returns a single mapping by device JID.
func (h *ChatwootConfigHandler) Get(c *fiber.Ctx) error {
	cfg, err := h.Repo.GetByDeviceID(deviceIDParam(c))
	if err != nil {
		return h.internalError(c, err)
	}
	if cfg == nil {
		return c.Status(fiber.StatusNotFound).JSON(utils.ResponseData{
			Code:    "NOT_FOUND",
			Message: "No Chatwoot config for that device",
		})
	}
	return c.JSON(utils.ResponseData{Code: "SUCCESS", Message: "Chatwoot device config", Results: withWebhookURL(cfg)})
}

// Create stores a new mapping. device_id and inbox_id are optional: an omitted
// device_id auto-selects the only connected device; an omitted inbox_id creates
// (or reuses by name) an API channel inbox from inbox_name. Idempotent: an
// existing mapping for the resolved device_id is updated rather than rejected.
func (h *ChatwootConfigHandler) Create(c *fiber.Ctx) error {
	var req chatwootConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ResponseError(c, "Invalid payload")
	}
	cfg := req.toConfig()

	// Auto-select the single connected device when device_id is omitted.
	if cfg.DeviceID == "" {
		deviceID, errData := h.autoResolveDeviceID()
		if errData != nil {
			return c.Status(errData.Status).JSON(errData)
		}
		cfg.DeviceID = deviceID
	}

	return h.save(c, &cfg, req.InboxName, fiber.StatusCreated, "Chatwoot device config created")
}

// Update overwrites the mapping for the device in the URL path. Like Create, it
// can auto-create/reuse the inbox when inbox_id is omitted but inbox_name is set.
func (h *ChatwootConfigHandler) Update(c *fiber.Ctx) error {
	var req chatwootConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ResponseError(c, "Invalid payload")
	}
	cfg := req.toConfig()
	cfg.DeviceID = deviceIDParam(c) // URL path is authoritative

	return h.save(c, &cfg, req.InboxName, fiber.StatusOK, "Chatwoot device config updated")
}

// save validates, resolves the inbox, persists, refreshes the registry and
// ensures the webhook is set. Shared by Create and Update.
func (h *ChatwootConfigHandler) save(c *fiber.Ctx, cfg *domainChatwoot.DeviceConfig, inboxName string, status int, message string) error {
	if cfg.DeviceID == "" {
		return utils.ResponseError(c, "device_id is required")
	}
	if cfg.ChatwootURL == "" || cfg.APIToken == "" {
		return utils.ResponseError(c, "chatwoot_url and api_token are required")
	}
	if cfg.AccountID <= 0 {
		return utils.ResponseError(c, "account_id must be a positive number (your Chatwoot account id, e.g. 2)")
	}
	if cfg.InboxID < 0 {
		return utils.ResponseError(c, "inbox_id must be a positive number")
	}

	// Resolve the inbox: explicit inbox_id wins; otherwise create/reuse by name.
	createdInbox, err := h.resolveInbox(cfg, inboxName)
	if err != nil {
		logrus.Warnf("Chatwoot: inbox resolution failed for account %d at %s: %v", cfg.AccountID, cfg.ChatwootURL, err)
		return c.Status(fiber.StatusBadGateway).JSON(utils.ResponseData{
			Status:  fiber.StatusBadGateway,
			Code:    "CHATWOOT_UNREACHABLE",
			Message: fmt.Sprintf("Could not reach the Chatwoot inbox API. Verify chatwoot_url, account_id and api_token. Detail: %v", err),
		})
	}
	if cfg.InboxID == 0 {
		return utils.ResponseError(c, "inbox_id or inbox_name is required")
	}

	if err := h.Repo.Save(cfg); err != nil {
		return h.internalError(c, err)
	}
	h.refresh()

	// A freshly created inbox already carries the webhook URL; only the explicit
	// or reused-by-name paths need an extra (idempotent) webhook update.
	if !createdInbox {
		autoRegisterWebhook(cfg)
	}

	return c.Status(status).JSON(utils.ResponseData{
		Code:    "SUCCESS",
		Message: message,
		Results: withWebhookURL(cfg),
	})
}

// autoResolveDeviceID returns the JID of the only connected device, or a
// describing error response (not yet written) when there are zero or multiple
// devices. It must not write to the context itself — c.JSON returns nil on
// success, so writing here would silently fail to short-circuit the caller.
func (h *ChatwootConfigHandler) autoResolveDeviceID() (string, *utils.ResponseData) {
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

// resolveInbox sets cfg.InboxID. With an explicit inbox_id it is a no-op. When
// inbox_id is omitted but inboxName is given, it reuses an inbox of that name or
// creates a new API channel inbox (with the webhook URL). Returns whether a new
// inbox was created.
func (h *ChatwootConfigHandler) resolveInbox(cfg *domainChatwoot.DeviceConfig, inboxName string) (bool, error) {
	inboxName = strings.TrimSpace(inboxName)
	if cfg.InboxID != 0 || inboxName == "" {
		return false, nil
	}
	client := chatwoot.NewClientFromConfig(cfg)
	existing, err := client.FindInboxByName(inboxName)
	if err != nil {
		return false, err
	}
	if existing != nil {
		cfg.InboxID = existing.ID
		return false, nil
	}
	created, err := client.CreateInbox(inboxName, webhookCallbackURL(cfg.DeviceID))
	if err != nil {
		return false, err
	}
	cfg.InboxID = created.ID
	logrus.Infof("Chatwoot: created API inbox %q (id %d) for device %s", inboxName, created.ID, cfg.DeviceID)
	return true, nil
}

// deviceJID returns a device's WhatsApp JID, falling back to its instance ID
// when not yet logged in.
func deviceJID(d *whatsapp.DeviceInstance) string {
	if jid := d.JID(); jid != "" {
		return jid
	}
	return d.ID()
}

// Delete removes the mapping for the device in the URL path.
func (h *ChatwootConfigHandler) Delete(c *fiber.Ctx) error {
	if err := h.Repo.Delete(deviceIDParam(c)); err != nil {
		return h.internalError(c, err)
	}
	h.refresh()
	return c.JSON(utils.ResponseData{Code: "SUCCESS", Message: "Chatwoot device config deleted"})
}

// webhookCallbackURL returns the public callback URL Chatwoot should POST to,
// derived from APP_PUBLIC_URL (+ base path), with the device_id appended as a
// query parameter so Chatwoot echoes it back on every webhook and GoWA can
// resolve which device the event belongs to. Empty when APP_PUBLIC_URL is unset.
func webhookCallbackURL(deviceID string) string {
	if config.AppPublicURL == "" {
		return ""
	}
	base := strings.TrimRight(config.AppPublicURL, "/") + config.AppBasePath + "/chatwoot/webhook"
	if deviceID == "" {
		return base
	}
	return base + "?device_id=" + url.QueryEscape(deviceID)
}

// withWebhookURL wraps a config with its derived webhook callback URL.
func withWebhookURL(cfg *domainChatwoot.DeviceConfig) chatwootConfigResponse {
	return chatwootConfigResponse{DeviceConfig: cfg, WebhookURL: webhookCallbackURL(cfg.DeviceID)}
}

// autoRegisterWebhook best-effort registers the GoWA callback URL on the
// Chatwoot inbox for this config. Non-blocking: failures are logged, not
// returned, so the mapping is still saved. No-op when APP_PUBLIC_URL is unset.
func autoRegisterWebhook(cfg *domainChatwoot.DeviceConfig) {
	cbURL := webhookCallbackURL(cfg.DeviceID)
	if cbURL == "" {
		logrus.Debug("Chatwoot: APP_PUBLIC_URL not set, skipping webhook auto-registration")
		return
	}
	client := chatwoot.NewClientFromConfig(cfg)
	if err := client.UpdateInboxWebhook(cfg.InboxID, cbURL); err != nil {
		logrus.Warnf("Chatwoot: failed to auto-register webhook on inbox %d (%s): %v", cfg.InboxID, cfg.DeviceID, err)
		return
	}
	logrus.Infof("Chatwoot: registered webhook %s on inbox %d (%s)", cbURL, cfg.InboxID, cfg.DeviceID)
}

func (h *ChatwootConfigHandler) refresh() {
	if h.Registry == nil {
		return
	}
	if err := h.Registry.Refresh(); err != nil {
		logrus.Warnf("Chatwoot: failed to refresh client registry: %v", err)
	}
}

func (h *ChatwootConfigHandler) internalError(c *fiber.Ctx, err error) error {
	logrus.Errorf("Chatwoot config: %v", err)
	return c.Status(fiber.StatusInternalServerError).JSON(utils.ResponseData{
		Code:    "INTERNAL_ERROR",
		Message: "Internal server error",
	})
}
