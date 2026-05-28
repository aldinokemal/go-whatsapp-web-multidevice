package webhook

// IDeviceWebhookRepository persists per-device webhook configurations.
// The implementation lives in infrastructure/webhook (dependency inversion):
// the domain owns the contract, infrastructure owns the SQLite details.
type IDeviceWebhookRepository interface {
	// Migrate ensures the backing table and index exist. Idempotent.
	Migrate() error
	// Save inserts the config when cfg.ID == 0 (filling cfg.ID with the new row id)
	// or updates the existing row when cfg.ID > 0.
	Save(cfg *DeviceWebhookConfig) error
	// GetByID returns the config with the given id, or (nil, nil) if none.
	GetByID(id int) (*DeviceWebhookConfig, error)
	// GetByDeviceID returns every config bound to a device JID (1:N).
	GetByDeviceID(deviceID string) ([]*DeviceWebhookConfig, error)
	// GetAll returns every stored config.
	GetAll() ([]*DeviceWebhookConfig, error)
	// Delete removes the config with the given id. No error if it does not exist.
	Delete(id int) error
}
