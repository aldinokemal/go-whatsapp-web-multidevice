package chatstorage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/sirupsen/logrus"
)

// PostgresDeviceRegistry keeps the device registry (device id ↔ JID, display name and
// per-device webhook) in the same Postgres database as the whatsmeow sessions, and
// delegates everything else (chats, messages, schedules) to the chat storage it wraps.
//
// Without it, pointing --db-uri at Postgres moves the sessions but not the registry:
// the registry stays in chatstorage.db, so moving the server (or losing the volume)
// leaves sessions whose custom device ids, names and webhooks are gone.
type PostgresDeviceRegistry struct {
	domainChatStorage.IChatStorageRepository
	db *sql.DB
}

const postgresDeviceRegistrySchema = `
CREATE TABLE IF NOT EXISTS gowa_devices (
	device_id                    TEXT PRIMARY KEY,
	display_name                 TEXT NOT NULL DEFAULT '',
	jid                          TEXT NOT NULL DEFAULT '',
	ad_jid                       TEXT NOT NULL DEFAULT '',
	webhook_url                  TEXT,
	webhook_secret               TEXT NOT NULL DEFAULT '',
	webhook_events               TEXT NOT NULL DEFAULT '',
	webhook_insecure_skip_verify BOOLEAN NOT NULL DEFAULT FALSE,
	created_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at                   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS gowa_devices_jid_idx ON gowa_devices (jid);
CREATE INDEX IF NOT EXISTS gowa_devices_ad_jid_idx ON gowa_devices (ad_jid);
`

// NewPostgresDeviceRegistry creates the registry table if needed and wraps base.
func NewPostgresDeviceRegistry(base domainChatStorage.IChatStorageRepository, db *sql.DB) (*PostgresDeviceRegistry, error) {
	if _, err := db.Exec(postgresDeviceRegistrySchema); err != nil {
		return nil, fmt.Errorf("create gowa_devices: %w", err)
	}
	return &PostgresDeviceRegistry{IChatStorageRepository: base, db: db}, nil
}

// ImportFrom moves the registry of src (the SQLite chat storage) here when this registry
// is still empty, webhook settings included, and then deletes the copied records from
// src. It returns how many records were moved.
func (r *PostgresDeviceRegistry) ImportFrom(src domainChatStorage.IChatStorageRepository) (int, error) {
	var count int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM gowa_devices`).Scan(&count); err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, nil
	}
	records, err := src.ListDeviceRecords()
	if err != nil {
		return 0, err
	}
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, rec := range records {
		cfg, err := src.GetDeviceWebhookConfig(rec.DeviceID)
		if err != nil {
			return 0, fmt.Errorf("webhook of %s: %w", rec.DeviceID, err)
		}
		if cfg == nil {
			cfg = &domainChatStorage.DeviceWebhookConfig{}
		}
		if _, err := tx.Exec(`
			INSERT INTO gowa_devices (device_id, display_name, jid, ad_jid, webhook_url, webhook_secret,
				webhook_events, webhook_insecure_skip_verify, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (device_id) DO NOTHING
		`, rec.DeviceID, rec.DisplayName, rec.JID, rec.ADJID, nonEmpty(cfg.WebhookURL), cfg.WebhookSecret,
			cfg.WebhookEvents, cfg.WebhookInsecureSkipVerify, rec.CreatedAt, rec.UpdatedAt); err != nil {
			return 0, fmt.Errorf("insert %s: %w", rec.DeviceID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	// The records live here now. Left in SQLite, an emptied registry would import
	// them again on a later boot and bring back devices removed in the meantime.
	for _, rec := range records {
		if err := src.DeleteDeviceRecord(rec.DeviceID); err != nil {
			return len(records), fmt.Errorf("copied, but clearing %s from the old registry failed: %w", rec.DeviceID, err)
		}
	}
	return len(records), nil
}

func nonEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

// SaveDeviceRecord upserts a device registration; webhook settings are left untouched.
func (r *PostgresDeviceRegistry) SaveDeviceRecord(record *domainChatStorage.DeviceRecord) error {
	if record == nil || strings.TrimSpace(record.DeviceID) == "" {
		return fmt.Errorf("device record with id is required")
	}
	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	_, err := r.db.Exec(`
		INSERT INTO gowa_devices (device_id, display_name, jid, ad_jid, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (device_id) DO UPDATE
		SET display_name = EXCLUDED.display_name, jid = EXCLUDED.jid, ad_jid = EXCLUDED.ad_jid,
		    updated_at = EXCLUDED.updated_at
	`, record.DeviceID, record.DisplayName, record.JID, record.ADJID, record.CreatedAt, record.UpdatedAt)
	return err
}

// ListDeviceRecords returns all registered devices, oldest first.
func (r *PostgresDeviceRegistry) ListDeviceRecords() ([]*domainChatStorage.DeviceRecord, error) {
	rows, err := r.db.Query(`
		SELECT device_id, display_name, jid, ad_jid, created_at, updated_at
		FROM gowa_devices
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*domainChatStorage.DeviceRecord
	for rows.Next() {
		var rec domainChatStorage.DeviceRecord
		if err := rows.Scan(&rec.DeviceID, &rec.DisplayName, &rec.JID, &rec.ADJID, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, &rec)
	}
	return records, rows.Err()
}

// GetDeviceRecord fetches a device registration by id; nil when absent.
func (r *PostgresDeviceRegistry) GetDeviceRecord(deviceID string) (*domainChatStorage.DeviceRecord, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, fmt.Errorf("device id is required")
	}
	rec := &domainChatStorage.DeviceRecord{}
	err := r.db.QueryRow(`
		SELECT device_id, display_name, jid, ad_jid, created_at, updated_at
		FROM gowa_devices WHERE device_id = $1
	`, deviceID).Scan(&rec.DeviceID, &rec.DisplayName, &rec.JID, &rec.ADJID, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// GetDeviceRecordByJID mirrors the SQLite behaviour: a full AD JID resolves the exact
// slot; a bare number resolves only while it is unambiguous.
func (r *PostgresDeviceRegistry) GetDeviceRecordByJID(jid string) (*domainChatStorage.DeviceRecord, error) {
	if strings.TrimSpace(jid) == "" {
		return nil, fmt.Errorf("jid is required")
	}
	rows, err := r.db.Query(`
		SELECT device_id, display_name, jid, ad_jid, webhook_url, webhook_secret, webhook_events,
		       webhook_insecure_skip_verify, created_at, updated_at
		FROM gowa_devices
		WHERE jid = $1 OR ad_jid = $1
		LIMIT 2
	`, jid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*domainChatStorage.DeviceRecord
	for rows.Next() {
		rec := &domainChatStorage.DeviceRecord{}
		if err := rows.Scan(&rec.DeviceID, &rec.DisplayName, &rec.JID, &rec.ADJID, &rec.WebhookURL,
			&rec.WebhookSecret, &rec.WebhookEvents, &rec.WebhookInsecureSkipVerify, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	switch len(records) {
	case 0:
		return nil, nil
	case 1:
		return records[0], nil
	default:
		logrus.Warnf("[CHATSTORAGE] %s matches multiple device slots; ignoring device-specific record (use the full AD JID or device id to disambiguate)", jid)
		return nil, nil
	}
}

// DeleteDeviceRecord removes a device registration entry.
func (r *PostgresDeviceRegistry) DeleteDeviceRecord(deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("device id is required")
	}
	_, err := r.db.Exec(`DELETE FROM gowa_devices WHERE device_id = $1`, deviceID)
	return err
}

// SetDeviceWebhookURL updates or clears the webhook URL; sql.ErrNoRows if the device is unknown.
func (r *PostgresDeviceRegistry) SetDeviceWebhookURL(deviceID string, webhookURL *string) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("device id is required")
	}
	result, err := r.db.Exec(`
		UPDATE gowa_devices SET webhook_url = $1, updated_at = $2 WHERE device_id = $3
	`, webhookURL, time.Now(), deviceID)
	return affectedOrNoRows(result, err)
}

// GetDeviceWebhookURL returns the device webhook, or nil when none is set.
func (r *PostgresDeviceRegistry) GetDeviceWebhookURL(deviceID string) (*string, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, fmt.Errorf("device id is required")
	}
	var webhookURL string
	err := r.db.QueryRow(`
		SELECT COALESCE(webhook_url, '') FROM gowa_devices WHERE device_id = $1
	`, deviceID).Scan(&webhookURL)
	if err == sql.ErrNoRows || (err == nil && webhookURL == "") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &webhookURL, nil
}

// SetDeviceWebhookConfig replaces the webhook configuration; sql.ErrNoRows if the device is unknown.
func (r *PostgresDeviceRegistry) SetDeviceWebhookConfig(deviceID string, config *domainChatStorage.DeviceWebhookConfig) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("device id is required")
	}
	if config == nil {
		return fmt.Errorf("webhook config is required")
	}
	result, err := r.db.Exec(`
		UPDATE gowa_devices
		SET webhook_url = $1, webhook_secret = $2, webhook_events = $3, webhook_insecure_skip_verify = $4, updated_at = $5
		WHERE device_id = $6
	`, nonEmpty(config.WebhookURL), config.WebhookSecret, config.WebhookEvents, config.WebhookInsecureSkipVerify, time.Now(), deviceID)
	return affectedOrNoRows(result, err)
}

// GetDeviceWebhookConfig returns the webhook configuration, or nil when the device is unknown.
func (r *PostgresDeviceRegistry) GetDeviceWebhookConfig(deviceID string) (*domainChatStorage.DeviceWebhookConfig, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, fmt.Errorf("device id is required")
	}
	var config domainChatStorage.DeviceWebhookConfig
	err := r.db.QueryRow(`
		SELECT webhook_url, webhook_secret, webhook_events, webhook_insecure_skip_verify
		FROM gowa_devices WHERE device_id = $1
	`, deviceID).Scan(&config.WebhookURL, &config.WebhookSecret, &config.WebhookEvents, &config.WebhookInsecureSkipVerify)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func affectedOrNoRows(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
