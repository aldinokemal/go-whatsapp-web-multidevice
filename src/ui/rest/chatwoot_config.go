package rest

import (
	"fmt"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
)

// chatwootConfigRequest is the PUT body for a per-device Chatwoot config. An
// empty api_token on update keeps the stored token (so other fields can be
// changed without re-sending the secret). enabled defaults to true on create.
type chatwootConfigRequest struct {
	ChatwootURL string `json:"chatwoot_url"`
	AccountID   int    `json:"account_id"`
	InboxID     int    `json:"inbox_id"`
	APIToken    string `json:"api_token"`
	Enabled     *bool  `json:"enabled"`
}

// maskAPIToken redacts a stored token for read responses, revealing only the
// last 4 characters so an operator can tell which token is set without exposing
// it.
func maskAPIToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 4 {
		return "****"
	}
	return "****" + token[len(token)-4:]
}

// perDeviceWebhookURL returns the URL an operator must set on the device's
// Chatwoot inbox so agent replies route back to this device. Derived from the
// configured public webhook base, or a relative path when none is set.
func perDeviceWebhookURL(deviceID string) string {
	if base := strings.TrimRight(strings.TrimSpace(config.ChatwootWebhookURL), "/"); base != "" {
		return base + "/" + deviceID
	}
	return strings.TrimRight(config.AppBasePath, "/") + "/chatwoot/webhook/" + deviceID
}

func chatwootConfigView(cfg *domainChatStorage.ChatwootDeviceConfig) map[string]any {
	return map[string]any{
		"device_id":    cfg.DeviceID,
		"device_jid":   cfg.DeviceJID,
		"chatwoot_url": cfg.ChatwootURL,
		"account_id":   cfg.AccountID,
		"inbox_id":     cfg.InboxID,
		"api_token":    maskAPIToken(cfg.APIToken),
		"enabled":      cfg.Enabled,
		"webhook_url":  perDeviceWebhookURL(cfg.DeviceID),
		"created_at":   cfg.CreatedAt,
		"updated_at":   cfg.UpdatedAt,
	}
}

// ListChatwootConfigs returns all per-device Chatwoot configs (tokens masked).
// GET /chatwoot/configs
func (h *ChatwootHandler) ListChatwootConfigs(c *fiber.Ctx) error {
	if h.ChatStorageRepo == nil {
		return utils.ResponseError(c, "storage not available")
	}
	configs, err := h.ChatStorageRepo.ListChatwootDeviceConfigs()
	if err != nil {
		return utils.ResponseError(c, fmt.Sprintf("failed to list configs: %v", err))
	}
	views := make([]map[string]any, 0, len(configs))
	for _, cfg := range configs {
		views = append(views, chatwootConfigView(cfg))
	}
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Chatwoot device configs", Results: views})
}

// GetChatwootConfig returns one device's Chatwoot config (token masked).
// GET /devices/:device_id/chatwoot/config
func (h *ChatwootHandler) GetChatwootConfig(c *fiber.Ctx) error {
	deviceID, ok := h.resolveConfigDeviceID(c)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(utils.ResponseData{Status: fiber.StatusNotFound, Code: "DEVICE_NOT_FOUND", Message: "device not found"})
	}
	cfg, err := h.ChatStorageRepo.GetChatwootDeviceConfig(deviceID)
	if err != nil {
		return utils.ResponseError(c, fmt.Sprintf("failed to load config: %v", err))
	}
	if cfg == nil {
		return c.Status(fiber.StatusNotFound).JSON(utils.ResponseData{Status: fiber.StatusNotFound, Code: "CONFIG_NOT_FOUND", Message: "no Chatwoot config for this device"})
	}
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Chatwoot device config", Results: chatwootConfigView(cfg)})
}

// UpsertChatwootConfig creates or updates a device's Chatwoot config.
// PUT /devices/:device_id/chatwoot/config
func (h *ChatwootHandler) UpsertChatwootConfig(c *fiber.Ctx) error {
	deviceID, ok := h.resolveConfigDeviceID(c)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(utils.ResponseData{Status: fiber.StatusNotFound, Code: "DEVICE_NOT_FOUND", Message: "device not found"})
	}

	var req chatwootConfigRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ResponseError(c, "Invalid request body")
	}

	// Validate + canonicalize the URL (also enforces SSRF restrictions).
	if err := chatwoot.ValidateChatwootURL(req.ChatwootURL); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{Status: fiber.StatusBadRequest, Code: "INVALID_CHATWOOT_URL", Message: err.Error()})
	}
	canonicalURL, _ := chatwoot.CanonicalizeChatwootURL(req.ChatwootURL)
	if req.AccountID <= 0 || req.InboxID <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{Status: fiber.StatusBadRequest, Code: "INVALID_REQUEST", Message: "account_id and inbox_id must be positive"})
	}

	existing, err := h.ChatStorageRepo.GetChatwootDeviceConfig(deviceID)
	if err != nil {
		return utils.ResponseError(c, fmt.Sprintf("failed to load existing config: %v", err))
	}

	// Token: keep the stored token when omitted on update; required on create.
	// A client that echoes back the masked value from a GET response also keeps
	// the stored token — otherwise the mask itself would silently become the
	// credential and every Chatwoot call would start failing with 401s.
	apiToken := strings.TrimSpace(req.APIToken)
	if apiToken == "" {
		if existing == nil {
			return c.Status(fiber.StatusBadRequest).JSON(utils.ResponseData{Status: fiber.StatusBadRequest, Code: "INVALID_REQUEST", Message: "api_token is required"})
		}
		apiToken = existing.APIToken
	} else if existing != nil && apiToken == maskAPIToken(existing.APIToken) {
		apiToken = existing.APIToken
	}

	// Guard against silently repointing historical conversations: changing the
	// routing identity (url/account/inbox) of a config that already has links is
	// rejected — create a new device config instead.
	if existing != nil {
		routingChanged := existing.ChatwootURL != canonicalURL || existing.AccountID != req.AccountID || existing.InboxID != req.InboxID
		if routingChanged {
			n, cErr := h.ChatStorageRepo.CountChatwootMessageLinksByConfig(existing.ID)
			if cErr != nil {
				return utils.ResponseError(c, fmt.Sprintf("failed to check existing links: %v", cErr))
			}
			if n > 0 {
				return c.Status(fiber.StatusConflict).JSON(utils.ResponseData{
					Status:  fiber.StatusConflict,
					Code:    "CONFIG_HAS_LINKED_CONVERSATIONS",
					Message: "cannot change chatwoot_url/account_id/inbox_id while linked conversations exist; delete and recreate the device config to rebind",
				})
			}
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	} else if existing != nil {
		enabled = existing.Enabled
	}

	cfg := &domainChatStorage.ChatwootDeviceConfig{
		DeviceID:    deviceID,
		DeviceJID:   h.deviceJID(deviceID),
		ChatwootURL: canonicalURL,
		AccountID:   req.AccountID,
		InboxID:     req.InboxID,
		APIToken:    apiToken,
		Enabled:     enabled,
	}
	if existing != nil {
		cfg.ID = existing.ID
		cfg.CreatedAt = existing.CreatedAt
	}

	if err := h.ChatStorageRepo.SaveChatwootDeviceConfig(cfg); err != nil {
		return utils.ResponseError(c, fmt.Sprintf("failed to save config: %v", err))
	}
	if reg := chatwoot.GetClientRegistry(); reg != nil {
		reg.Invalidate(deviceID)
	}

	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Chatwoot device config saved", Results: chatwootConfigView(cfg)})
}

// DeleteChatwootConfig removes a device's Chatwoot config, along with the
// message links written under it. Stale links must not survive the config:
// after a delete-and-recreate rebind they would still win the account-scoped
// reverse lookup and hijack reply destinations toward the old mapping.
// DELETE /devices/:device_id/chatwoot/config
func (h *ChatwootHandler) DeleteChatwootConfig(c *fiber.Ctx) error {
	// Resolve aliases/JIDs the same way GET and PUT do — a raw JID param would
	// otherwise delete nothing and still report success. Fall back to the raw
	// param so a config orphaned by device removal stays deletable.
	deviceID, ok := h.resolveConfigDeviceID(c)
	if !ok {
		deviceID = strings.TrimSpace(c.Params("device_id"))
	}
	if deviceID == "" {
		return utils.ResponseError(c, "device_id is required")
	}
	cfg, err := h.ChatStorageRepo.GetChatwootDeviceConfig(deviceID)
	if err != nil {
		return utils.ResponseError(c, fmt.Sprintf("failed to load config: %v", err))
	}
	if err := h.ChatStorageRepo.DeleteChatwootDeviceConfig(deviceID); err != nil {
		return utils.ResponseError(c, fmt.Sprintf("failed to delete config: %v", err))
	}
	if cfg != nil && cfg.ID != 0 {
		if err := h.ChatStorageRepo.DeleteChatwootMessageLinksByConfig(cfg.ID); err != nil {
			logrus.Errorf("Chatwoot: failed to delete message links for config %d: %v", cfg.ID, err)
		}
	}
	if reg := chatwoot.GetClientRegistry(); reg != nil {
		reg.Invalidate(deviceID)
	}
	return c.JSON(utils.ResponseData{Status: 200, Code: "SUCCESS", Message: "Chatwoot device config deleted", Results: map[string]any{"device_id": deviceID}})
}

// resolveConfigDeviceID resolves the :device_id path param to a known device id
// (DeviceMiddleware reads only header/query, so config routes resolve manually).
//
// The result is cloned: when ResolveDevice matches by exact id it returns the
// param-derived string itself, whose backing buffer fasthttp recycles after the
// request. Handlers persist this id (config rows, registry cache), so an
// uncopied value would mutate under the next request.
func (h *ChatwootHandler) resolveConfigDeviceID(c *fiber.Ctx) (string, bool) {
	deviceID := strings.TrimSpace(c.Params("device_id"))
	if deviceID == "" || h.DeviceManager == nil {
		return "", false
	}
	_, resolvedID, err := h.DeviceManager.ResolveDevice(deviceID)
	if err != nil {
		return "", false
	}
	return strings.Clone(resolvedID), true
}

// deviceJID returns the WhatsApp storage JID for a device, used so the registry
// can resolve a config by either the device id or the JID. Empty before login.
func (h *ChatwootHandler) deviceJID(deviceID string) string {
	if h.DeviceManager == nil {
		return ""
	}
	instance, _, err := h.DeviceManager.ResolveDevice(deviceID)
	if err != nil || instance == nil {
		return ""
	}
	return instance.JID()
}
