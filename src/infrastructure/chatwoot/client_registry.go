package chatwoot

import (
	"errors"
	"strings"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// ErrClientRegistryUnavailable is returned when a Chatwoot forward is attempted
// before the process-wide client registry has been initialized. It is a wiring/
// startup condition, not a transient network failure, so callers surface it
// loudly instead of treating a nil registry as "device has no config" and
// silently dropping the message: the retry worker reschedules the job (rather
// than marking it done and deleting it), and the live forward path logs without
// enqueuing a doomed retry (Retryable reports false for it).
var ErrClientRegistryUnavailable = errors.New("chatwoot: client registry not initialized")

// ResolvedConfig is the outcome of resolving a device (or inbox) to a Chatwoot
// destination. ConfigID is the chatwoot_device_configs row id, or 0 for the
// legacy/env config used when no per-device config rows exist.
//
// IgnoreJids, SignMsg and SignDelimiter carry the device's optional behaviour
// settings (see ChatwootDeviceConfig) so the per-message paths need no extra
// storage lookup. Read them through EffectiveIgnoreJids (matched with
// ShouldIgnoreMessage), SignMsgEnabled and SignDelimiterValue, which apply the global CHATWOOT_* fallback and accept a
// nil receiver.
type ResolvedConfig struct {
	ConfigID int64
	DeviceID string
	Client   *Client

	IgnoreJids    []string
	SignMsg       *bool
	SignDelimiter *string
}

// newResolvedConfig builds the resolved view of a stored per-device config.
func newResolvedConfig(cfg *domainChatStorage.ChatwootDeviceConfig) *ResolvedConfig {
	return &ResolvedConfig{
		ConfigID:      cfg.ID,
		DeviceID:      cfg.DeviceID,
		Client:        NewClientFromConfig(cfg.ChatwootURL, cfg.APIToken, cfg.AccountID, cfg.InboxID),
		IgnoreJids:    cfg.IgnoreJids,
		SignMsg:       cfg.SignMsg,
		SignDelimiter: cfg.SignDelimiter,
	}
}

// SignMsgEnabled reports whether agent replies sent from this device are
// prefixed with the agent's name: the device's sign_msg when set, else
// CHATWOOT_SIGN_MSG.
func (rc *ResolvedConfig) SignMsgEnabled() bool {
	if rc != nil && rc.SignMsg != nil {
		return *rc.SignMsg
	}
	return config.ChatwootSignMsg
}

// SignDelimiterValue returns the raw delimiter placed between the agent
// signature and the reply body: the device's sign_delimiter when set, else
// CHATWOOT_SIGN_DELIMITER.
func (rc *ResolvedConfig) SignDelimiterValue() string {
	if rc != nil && rc.SignDelimiter != nil {
		return *rc.SignDelimiter
	}
	return config.ChatwootSignDelimiter
}

// ClientRegistry resolves a per-device Chatwoot *Client (plus its scope) from
// the chatwoot_device_configs table, caching built clients. It replaces the
// process-global GetDefaultClient singleton.
//
// Resolution is fail-fast: once any per-device config row exists, an unmapped
// device resolves to nil (caller skips / errors) rather than silently falling
// back to the global env inbox — which would mis-deliver across accounts. The
// env config is only used as a single "legacy" config while the table is empty.
type ClientRegistry struct {
	mu    sync.RWMutex
	cache map[string]*ResolvedConfig
	repo  domainChatStorage.IChatStorageRepository
}

func NewClientRegistry(repo domainChatStorage.IChatStorageRepository) *ClientRegistry {
	return &ClientRegistry{
		cache: make(map[string]*ResolvedConfig),
		repo:  repo,
	}
}

// Resolve maps a device identifier (user-facing device id OR WhatsApp JID) to a
// Chatwoot client. Returns (nil, nil) when the device has no usable config and
// the env fallback does not apply (fail-fast).
func (r *ClientRegistry) Resolve(identifier string) (*ResolvedConfig, error) {
	// The identifier is retained past this call (cache map key, ResolvedConfig
	// DeviceID). Callers on the fiber paths hand us c.Params()/body-derived
	// strings whose backing buffer fasthttp recycles after the request — without
	// a copy the cached key's bytes would silently mutate under the next request.
	identifier = strings.Clone(strings.TrimSpace(identifier))

	r.mu.RLock()
	if rc, ok := r.cache[identifier]; ok {
		r.mu.RUnlock()
		return rc, nil
	}
	r.mu.RUnlock()

	if r.repo == nil {
		return nil, nil
	}

	cfg, err := r.repo.GetChatwootDeviceConfigByIdentifier(identifier)
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		if !cfg.Enabled {
			// Explicitly disabled for this device: do not forward, do not fall back.
			return nil, nil
		}
		rc := newResolvedConfig(cfg)
		r.store(identifier, rc)
		return rc, nil
	}

	// No per-device row. Fall back to the env config ONLY while the table is
	// empty (pure legacy/single-device mode).
	count, err := r.repo.CountChatwootDeviceConfigs()
	if err != nil {
		return nil, err
	}
	if count == 0 {
		rc := &ResolvedConfig{ConfigID: 0, DeviceID: identifier, Client: NewClient()}
		r.store(identifier, rc)
		return rc, nil
	}
	return nil, nil
}

// ResolveByInbox maps a Chatwoot (account, inbox) to a device config. Used by
// the reverse path for agent-initiated conversations. Returns nil when there is
// no unambiguous match (the underlying repo returns nil on ambiguity).
func (r *ClientRegistry) ResolveByInbox(accountID, inboxID int) (*ResolvedConfig, error) {
	if r.repo == nil {
		return nil, nil
	}
	cfg, err := r.repo.GetChatwootDeviceConfigByInbox(accountID, inboxID)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	return newResolvedConfig(cfg), nil
}

// Invalidate drops every cached entry for a device so the next Resolve rebuilds
// the client with fresh credentials. Called after a config write/delete.
//
// Env-fallback entries (ConfigID 0) are always purged too: they were cached
// while the config table was empty, under whatever identifier the caller used
// (often a JID, with DeviceID set to that same identifier), so a device-id
// match can never find them. Leaving them would keep routing forwards to the
// env inbox after the first per-device config is written — exactly the
// mis-delivery the fail-fast contract exists to prevent.
func (r *ClientRegistry) Invalidate(deviceID string) {
	deviceID = strings.TrimSpace(deviceID)
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, rc := range r.cache {
		if key == deviceID || rc == nil || rc.DeviceID == deviceID || rc.ConfigID == 0 {
			delete(r.cache, key)
		}
	}
}

func (r *ClientRegistry) store(identifier string, rc *ResolvedConfig) {
	r.mu.Lock()
	r.cache[identifier] = rc
	r.mu.Unlock()
}

// Package-global registry, initialized at boot from the chat storage repo.
var (
	globalRegistry   *ClientRegistry
	globalRegistryMu sync.RWMutex
)

// InitClientRegistry installs the process-wide registry. Call once at startup.
func InitClientRegistry(repo domainChatStorage.IChatStorageRepository) {
	globalRegistryMu.Lock()
	defer globalRegistryMu.Unlock()
	globalRegistry = NewClientRegistry(repo)
}

// GetClientRegistry returns the process-wide registry (nil before init).
func GetClientRegistry() *ClientRegistry {
	globalRegistryMu.RLock()
	defer globalRegistryMu.RUnlock()
	return globalRegistry
}
