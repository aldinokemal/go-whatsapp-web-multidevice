package webhook

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	domainWebhook "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/webhook"
)

// DeviceWebhookRepository is a SQLite-backed implementation of
// domainWebhook.IDeviceWebhookRepository. It owns a single table
// (device_webhook_configs) and creates it on demand via Migrate(), so it does
// not interfere with the chatstorage schema_info migration chain even when it
// shares the same *sql.DB connection.
type DeviceWebhookRepository struct {
	db *sql.DB
}

// NewDeviceWebhookRepository creates a repository over the given DB connection.
func NewDeviceWebhookRepository(db *sql.DB) domainWebhook.IDeviceWebhookRepository {
	return &DeviceWebhookRepository{db: db}
}

// Migrate creates the configs table and the device lookup index if missing.
func (r *DeviceWebhookRepository) Migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS device_webhook_configs (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id   TEXT    NOT NULL,
			webhook_url TEXT    NOT NULL,
			secret      TEXT    NOT NULL DEFAULT '',
			events      TEXT    NOT NULL DEFAULT '',
			enabled     BOOLEAN NOT NULL DEFAULT 1,
			headers     TEXT    NOT NULL DEFAULT '',
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_device_webhook_configs_device
			ON device_webhook_configs(device_id)`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.Exec(stmt); err != nil {
			return fmt.Errorf("device webhook config migrate: %w", err)
		}
	}
	return nil
}

// Save inserts a new config (cfg.ID == 0) or updates an existing one (cfg.ID > 0).
func (r *DeviceWebhookRepository) Save(cfg *domainWebhook.DeviceWebhookConfig) error {
	if cfg == nil || cfg.DeviceID == "" {
		return fmt.Errorf("device webhook config: device_id is required")
	}
	if cfg.WebhookURL == "" {
		return fmt.Errorf("device webhook config: webhook_url is required")
	}

	events := encodeEvents(cfg.Events)
	headers, err := encodeHeaders(cfg.Headers)
	if err != nil {
		return fmt.Errorf("device webhook config: %w", err)
	}

	if cfg.ID == 0 {
		res, err := r.db.Exec(`
			INSERT INTO device_webhook_configs
				(device_id, webhook_url, secret, events, enabled, headers)
			VALUES (?, ?, ?, ?, ?, ?)
		`, cfg.DeviceID, cfg.WebhookURL, cfg.Secret, events, cfg.Enabled, headers)
		if err != nil {
			return fmt.Errorf("device webhook config insert: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("device webhook config insert id: %w", err)
		}
		cfg.ID = int(id)
		return nil
	}

	_, err = r.db.Exec(`
		UPDATE device_webhook_configs SET
			device_id   = ?,
			webhook_url = ?,
			secret      = ?,
			events      = ?,
			enabled     = ?,
			headers     = ?,
			updated_at  = CURRENT_TIMESTAMP
		WHERE id = ?
	`, cfg.DeviceID, cfg.WebhookURL, cfg.Secret, events, cfg.Enabled, headers, cfg.ID)
	if err != nil {
		return fmt.Errorf("device webhook config update: %w", err)
	}
	return nil
}

// GetByID returns the config with the given id, or (nil, nil) if absent.
func (r *DeviceWebhookRepository) GetByID(id int) (*domainWebhook.DeviceWebhookConfig, error) {
	row := r.db.QueryRow(`
		SELECT id, device_id, webhook_url, secret, events, enabled, headers
		FROM device_webhook_configs WHERE id = ?
	`, id)
	return scanWebhookConfig(row)
}

// GetByDeviceID returns every config bound to a device JID, ordered by id.
func (r *DeviceWebhookRepository) GetByDeviceID(deviceID string) ([]*domainWebhook.DeviceWebhookConfig, error) {
	rows, err := r.db.Query(`
		SELECT id, device_id, webhook_url, secret, events, enabled, headers
		FROM device_webhook_configs WHERE device_id = ? ORDER BY id
	`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("device webhook config by device: %w", err)
	}
	return scanWebhookConfigs(rows)
}

// GetAll returns every stored config ordered by id.
func (r *DeviceWebhookRepository) GetAll() ([]*domainWebhook.DeviceWebhookConfig, error) {
	rows, err := r.db.Query(`
		SELECT id, device_id, webhook_url, secret, events, enabled, headers
		FROM device_webhook_configs ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("device webhook config list: %w", err)
	}
	return scanWebhookConfigs(rows)
}

// Delete removes the config with the given id. Missing rows are not an error.
func (r *DeviceWebhookRepository) Delete(id int) error {
	if _, err := r.db.Exec(`DELETE FROM device_webhook_configs WHERE id = ?`, id); err != nil {
		return fmt.Errorf("device webhook config delete: %w", err)
	}
	return nil
}

// scanWebhookConfig maps a single row, returning (nil, nil) when no row exists.
func scanWebhookConfig(row *sql.Row) (*domainWebhook.DeviceWebhookConfig, error) {
	cfg := &domainWebhook.DeviceWebhookConfig{}
	var events, headers string
	err := row.Scan(&cfg.ID, &cfg.DeviceID, &cfg.WebhookURL, &cfg.Secret, &events, &cfg.Enabled, &headers)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("device webhook config get: %w", err)
	}
	cfg.Events = decodeEvents(events)
	cfg.Headers, err = decodeHeaders(headers)
	if err != nil {
		return nil, fmt.Errorf("device webhook config decode headers: %w", err)
	}
	return cfg, nil
}

// scanWebhookConfigs maps multiple rows and closes the result set.
func scanWebhookConfigs(rows *sql.Rows) ([]*domainWebhook.DeviceWebhookConfig, error) {
	defer rows.Close()
	var configs []*domainWebhook.DeviceWebhookConfig
	for rows.Next() {
		cfg := &domainWebhook.DeviceWebhookConfig{}
		var events, headers string
		if err := rows.Scan(&cfg.ID, &cfg.DeviceID, &cfg.WebhookURL, &cfg.Secret, &events, &cfg.Enabled, &headers); err != nil {
			return nil, fmt.Errorf("device webhook config scan: %w", err)
		}
		cfg.Events = decodeEvents(events)
		decoded, err := decodeHeaders(headers)
		if err != nil {
			return nil, fmt.Errorf("device webhook config decode headers: %w", err)
		}
		cfg.Headers = decoded
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

// encodeEvents joins the event list into the comma-separated TEXT column,
// trimming blanks. An empty list yields "" (meaning "all events").
func encodeEvents(events []string) string {
	cleaned := make([]string, 0, len(events))
	for _, e := range events {
		if e = strings.TrimSpace(e); e != "" {
			cleaned = append(cleaned, e)
		}
	}
	return strings.Join(cleaned, ",")
}

// decodeEvents splits the stored TEXT column back into a slice, dropping blanks.
// An empty column yields a nil slice (meaning "all events").
func decodeEvents(events string) []string {
	if strings.TrimSpace(events) == "" {
		return nil
	}
	parts := strings.Split(events, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// encodeHeaders marshals the header map to the JSON TEXT column. A nil/empty map
// yields "" so the column stays clean.
func encodeHeaders(headers map[string]string) (string, error) {
	if len(headers) == 0 {
		return "", nil
	}
	b, err := json.Marshal(headers)
	if err != nil {
		return "", fmt.Errorf("encode headers: %w", err)
	}
	return string(b), nil
}

// decodeHeaders unmarshals the JSON TEXT column back into a map. An empty column
// yields a nil map.
func decodeHeaders(headers string) (map[string]string, error) {
	if strings.TrimSpace(headers) == "" {
		return nil, nil
	}
	out := make(map[string]string)
	if err := json.Unmarshal([]byte(headers), &out); err != nil {
		return nil, fmt.Errorf("decode headers: %w", err)
	}
	return out, nil
}
