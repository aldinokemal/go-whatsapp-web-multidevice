package chatwoot

// IDeviceConfigRepository persists per-device Chatwoot configurations.
// The implementation lives in infrastructure/chatwoot (dependency inversion):
// the domain owns the contract, infrastructure owns the SQLite details.
type IDeviceConfigRepository interface {
	// Migrate ensures the backing table and indexes exist. Idempotent.
	Migrate() error
	// Save inserts or updates the config for cfg.DeviceID (upsert by device_id).
	Save(cfg *DeviceConfig) error
	// GetByDeviceID returns the config for a device JID, or (nil, nil) if none.
	GetByDeviceID(deviceID string) (*DeviceConfig, error)
	// GetByInboxID returns the enabled config bound to a Chatwoot account+inbox,
	// or (nil, nil) if none. Used to route inbound Chatwoot webhooks to a device.
	GetByInboxID(accountID, inboxID int) (*DeviceConfig, error)
	// GetAll returns every stored config.
	GetAll() ([]*DeviceConfig, error)
	// Delete removes the config for a device JID. No error if it does not exist.
	Delete(deviceID string) error
}
