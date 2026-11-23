package store

import (
	"context"
	"database/sql"
	"errors"
)

const SettingServiceDeviceID = "service_device_id"

func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value, by string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_by, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		key, value, by, s.now())
	return err
}
