package webhook

import (
	"sync"

	domainWebhook "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/webhook"
)

// WebhookRegistry resolves the per-device webhook configs for a given device.
// It caches enabled configs in memory and is refreshed after every CRUD write.
//
// Single responsibility: it only answers "which webhooks does this device own?".
// It deliberately does NOT fall back to the global WHATSAPP_WEBHOOK; that
// backward-compatibility decision lives in the forwarding layer (whatsapp), which
// uses the global webhooks when a device has no per-device config.
type WebhookRegistry struct {
	mu       sync.RWMutex
	byDevice map[string][]domainWebhook.DeviceWebhookConfig // only enabled configs
	repo     domainWebhook.IDeviceWebhookRepository
}

// NewWebhookRegistry builds a registry and warms its cache from the repository.
func NewWebhookRegistry(repo domainWebhook.IDeviceWebhookRepository) *WebhookRegistry {
	r := &WebhookRegistry{
		byDevice: make(map[string][]domainWebhook.DeviceWebhookConfig),
		repo:     repo,
	}
	_ = r.Refresh()
	return r
}

// GetWebhooksForDevice returns the enabled webhook configs bound to deviceID,
// or an empty slice when the device has none.
func (r *WebhookRegistry) GetWebhooksForDevice(deviceID string) []domainWebhook.DeviceWebhookConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byDevice[deviceID]
}

// Refresh reloads the cache from the repository. Call after any CRUD write.
func (r *WebhookRegistry) Refresh() error {
	if r.repo == nil {
		return nil
	}
	configs, err := r.repo.GetAll()
	if err != nil {
		return err
	}
	byDevice := make(map[string][]domainWebhook.DeviceWebhookConfig)
	for _, cfg := range configs {
		if cfg == nil || !cfg.Enabled {
			continue
		}
		byDevice[cfg.DeviceID] = append(byDevice[cfg.DeviceID], *cfg)
	}
	r.mu.Lock()
	r.byDevice = byDevice
	r.mu.Unlock()
	return nil
}

// globalRegistry holds the process-wide per-device webhook registry. It is set
// once at boot (cmd) and read by the forward path, matching the package style
// used by the Chatwoot integration (chatwoot.SetGlobalRegistry).
var globalRegistry *WebhookRegistry

// SetGlobalRegistry installs the process-wide webhook registry. Called once at boot.
func SetGlobalRegistry(r *WebhookRegistry) { globalRegistry = r }

// GetGlobalRegistry returns the process-wide webhook registry, or nil if unset.
func GetGlobalRegistry() *WebhookRegistry { return globalRegistry }
