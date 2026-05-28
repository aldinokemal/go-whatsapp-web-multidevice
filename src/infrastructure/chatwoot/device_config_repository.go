package chatwoot

import (
	"database/sql"
	"fmt"

	domainChatwoot "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatwoot"
)

// DeviceConfigRepository is a SQLite-backed implementation of
// domainChatwoot.IDeviceConfigRepository. It owns a single table
// (chatwoot_device_configs) and creates it on demand via Migrate(), so it does
// not interfere with the chatstorage schema_info migration chain even when it
// shares the same *sql.DB connection.
type DeviceConfigRepository struct {
	db *sql.DB
}

// NewDeviceConfigRepository creates a repository over the given DB connection.
func NewDeviceConfigRepository(db *sql.DB) domainChatwoot.IDeviceConfigRepository {
	return &DeviceConfigRepository{db: db}
}

// Migrate creates the configs table and the inbox lookup index if missing.
func (r *DeviceConfigRepository) Migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS chatwoot_device_configs (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id       TEXT    NOT NULL UNIQUE,
			chatwoot_url    TEXT    NOT NULL,
			api_token       TEXT    NOT NULL,
			account_id      INTEGER NOT NULL,
			inbox_id        INTEGER NOT NULL,
			enabled         BOOLEAN NOT NULL DEFAULT 1,
			import_messages BOOLEAN NOT NULL DEFAULT 0,
			days_limit      INTEGER NOT NULL DEFAULT 3,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chatwoot_device_configs_inbox
			ON chatwoot_device_configs(account_id, inbox_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_chatwoot_device_configs_inbox_unique
			ON chatwoot_device_configs(account_id, inbox_id) WHERE enabled = 1`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.Exec(stmt); err != nil {
			return fmt.Errorf("chatwoot device config migrate: %w", err)
		}
	}
	return nil
}

// Save upserts the config keyed by device_id.
func (r *DeviceConfigRepository) Save(cfg *domainChatwoot.DeviceConfig) error {
	if cfg == nil || cfg.DeviceID == "" {
		return fmt.Errorf("chatwoot device config: device_id is required")
	}
	if cfg.ChatwootURL == "" {
		return fmt.Errorf("chatwoot device config: chatwoot_url is required")
	}
	if cfg.APIToken == "" {
		return fmt.Errorf("chatwoot device config: api_token is required")
	}
	if cfg.AccountID == 0 {
		return fmt.Errorf("chatwoot device config: account_id is required")
	}
	if cfg.InboxID == 0 {
		return fmt.Errorf("chatwoot device config: inbox_id is required")
	}

	// Enforce one enabled device per (account, inbox). The partial unique index
	// would reject this anyway, but a pre-check yields a clearer error message.
	if cfg.Enabled {
		var otherDeviceID string
		switch err := r.db.QueryRow(`
			SELECT device_id FROM chatwoot_device_configs
			WHERE account_id = ? AND inbox_id = ? AND enabled = 1 AND device_id <> ?
			LIMIT 1
		`, cfg.AccountID, cfg.InboxID, cfg.DeviceID).Scan(&otherDeviceID); {
		case err == nil:
			return fmt.Errorf("chatwoot device config: inbox %d in account %d is already mapped to enabled device %s", cfg.InboxID, cfg.AccountID, otherDeviceID)
		case err != sql.ErrNoRows:
			return fmt.Errorf("chatwoot device config conflict check: %w", err)
		}
	}

	_, err := r.db.Exec(`
		INSERT INTO chatwoot_device_configs
			(device_id, chatwoot_url, api_token, account_id, inbox_id, enabled, import_messages, days_limit, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(device_id) DO UPDATE SET
			chatwoot_url    = excluded.chatwoot_url,
			api_token       = excluded.api_token,
			account_id      = excluded.account_id,
			inbox_id        = excluded.inbox_id,
			enabled         = excluded.enabled,
			import_messages = excluded.import_messages,
			days_limit      = excluded.days_limit,
			updated_at      = CURRENT_TIMESTAMP
	`, cfg.DeviceID, cfg.ChatwootURL, cfg.APIToken, cfg.AccountID, cfg.InboxID, cfg.Enabled, cfg.ImportMessages, cfg.DaysLimit)
	if err != nil {
		return fmt.Errorf("chatwoot device config save: %w", err)
	}
	return nil
}

// GetByDeviceID returns the config for a device JID, or (nil, nil) if absent.
func (r *DeviceConfigRepository) GetByDeviceID(deviceID string) (*domainChatwoot.DeviceConfig, error) {
	row := r.db.QueryRow(`
		SELECT id, device_id, chatwoot_url, api_token, account_id, inbox_id, enabled, import_messages, days_limit
		FROM chatwoot_device_configs WHERE device_id = ?
	`, deviceID)
	return scanConfig(row)
}

// GetByInboxID returns the enabled config for an account+inbox, or (nil, nil).
func (r *DeviceConfigRepository) GetByInboxID(accountID, inboxID int) (*domainChatwoot.DeviceConfig, error) {
	row := r.db.QueryRow(`
		SELECT id, device_id, chatwoot_url, api_token, account_id, inbox_id, enabled, import_messages, days_limit
		FROM chatwoot_device_configs
		WHERE account_id = ? AND inbox_id = ? AND enabled = 1
		LIMIT 1
	`, accountID, inboxID)
	return scanConfig(row)
}

// GetAll returns every stored config ordered by id.
func (r *DeviceConfigRepository) GetAll() ([]*domainChatwoot.DeviceConfig, error) {
	rows, err := r.db.Query(`
		SELECT id, device_id, chatwoot_url, api_token, account_id, inbox_id, enabled, import_messages, days_limit
		FROM chatwoot_device_configs ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("chatwoot device config list: %w", err)
	}
	defer rows.Close()

	var configs []*domainChatwoot.DeviceConfig
	for rows.Next() {
		cfg := &domainChatwoot.DeviceConfig{}
		if err := rows.Scan(&cfg.ID, &cfg.DeviceID, &cfg.ChatwootURL, &cfg.APIToken,
			&cfg.AccountID, &cfg.InboxID, &cfg.Enabled, &cfg.ImportMessages, &cfg.DaysLimit); err != nil {
			return nil, fmt.Errorf("chatwoot device config scan: %w", err)
		}
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

// Delete removes the config for a device JID. Missing rows are not an error.
func (r *DeviceConfigRepository) Delete(deviceID string) error {
	if _, err := r.db.Exec(`DELETE FROM chatwoot_device_configs WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("chatwoot device config delete: %w", err)
	}
	return nil
}

// scanConfig maps a single row, returning (nil, nil) when no row exists.
func scanConfig(row *sql.Row) (*domainChatwoot.DeviceConfig, error) {
	cfg := &domainChatwoot.DeviceConfig{}
	err := row.Scan(&cfg.ID, &cfg.DeviceID, &cfg.ChatwootURL, &cfg.APIToken,
		&cfg.AccountID, &cfg.InboxID, &cfg.Enabled, &cfg.ImportMessages, &cfg.DaysLimit)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("chatwoot device config get: %w", err)
	}
	return cfg, nil
}
