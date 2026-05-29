package chatwoot

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	domainChatwoot "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatwoot"
)

// ErrNoConfig is returned when no Chatwoot configuration (per-device nor global
// env-var default) can satisfy a lookup.
var ErrNoConfig = errors.New("chatwoot: no configuration available")

// ClientRegistry resolves the correct *Client for a given device or inbox,
// replacing the single env-var-backed GetDefaultClient() singleton. It caches
// clients in memory and falls back to the env-var default for backward
// compatibility when a device has no explicit config.
type ClientRegistry struct {
	mu      sync.RWMutex
	clients map[string]*Client // key: device_id (WhatsApp JID)
	repo    domainChatwoot.IDeviceConfigRepository
}

// NewClientRegistry builds a registry and warms its cache from the repository.
func NewClientRegistry(repo domainChatwoot.IDeviceConfigRepository) *ClientRegistry {
	r := &ClientRegistry{
		clients: make(map[string]*Client),
		repo:    repo,
	}
	_ = r.Refresh()
	return r
}

// GetClientForDevice returns the client configured for deviceID. Resolution
// order: in-memory cache, repository lookup, env-var default (backward compat),
// then ErrNoConfig.
func (r *ClientRegistry) GetClientForDevice(deviceID string) (*Client, error) {
	r.mu.RLock()
	cached, ok := r.clients[deviceID]
	r.mu.RUnlock()
	if ok {
		return cached, nil
	}

	if r.repo != nil {
		cfg, err := r.repo.GetByDeviceID(deviceID)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			// A config row exists but is explicitly disabled: respect that
			// decision and signal "no client" rather than falling back to the
			// env-var default, which would silently re-enable the device.
			if !cfg.Enabled {
				return nil, nil
			}
			client := NewClientFromConfig(cfg)
			r.mu.Lock()
			r.clients[deviceID] = client
			r.mu.Unlock()
			return client, nil
		}
	}

	if def := GetDefaultClient(); def.IsConfigured() {
		return def, nil
	}

	return nil, ErrNoConfig
}

// GetClientForInbox returns the client and device_id bound to a Chatwoot
// account+inbox pair, used to route inbound Chatwoot webhooks to a device.
func (r *ClientRegistry) GetClientForInbox(accountID, inboxID int) (*Client, string, error) {
	if r.repo != nil {
		cfg, err := r.repo.GetByInboxID(accountID, inboxID)
		if err != nil {
			return nil, "", err
		}
		if cfg != nil {
			return NewClientFromConfig(cfg), cfg.DeviceID, nil
		}
	}
	return nil, "", ErrNoConfig
}

// Refresh reloads the cache from the repository. Call after any CRUD write.
func (r *ClientRegistry) Refresh() error {
	if r.repo == nil {
		return nil
	}
	configs, err := r.repo.GetAll()
	if err != nil {
		return err
	}
	clients := make(map[string]*Client, len(configs))
	for _, cfg := range configs {
		if cfg.Enabled {
			clients[cfg.DeviceID] = NewClientFromConfig(cfg)
		}
	}
	r.mu.Lock()
	r.clients = clients
	r.mu.Unlock()
	return nil
}

// NewClientFromConfig builds a *Client from a stored per-device config,
// mirroring NewClient() but sourcing values from the config row.
func NewClientFromConfig(cfg *domainChatwoot.DeviceConfig) *Client {
	return &Client{
		BaseURL:   strings.TrimRight(cfg.ChatwootURL, "/"),
		APIToken:  cfg.APIToken,
		AccountID: cfg.AccountID,
		InboxID:   cfg.InboxID,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}
