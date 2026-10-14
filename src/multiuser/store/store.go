// Package store holds WTF's accounts database (storages/accounts.db).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db  *sql.DB
	Now func() time.Time
}

func Open(uri string) (*Store, error) {
	db, err := sql.Open(sqlite.DriverName, sqlite.FormatChatStorageURI(uri, true, true))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("accounts db: %w", err)
	}
	st := &Store{db: db, Now: func() time.Time { return time.Now().UTC() }}
	if err := st.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("accounts db migrate: %w", err)
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrations run in order, once each. Append only; never edit a shipped entry.
var migrations = []string{
	`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		phone TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'active',
		max_devices INTEGER NOT NULL DEFAULT 2,
		created_at TIMESTAMP NOT NULL,
		last_login_at TIMESTAMP NULL)`,
	`CREATE TABLE login_tokens (
		token_hash TEXT PRIMARY KEY,
		phone TEXT NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		used_at TIMESTAMP NULL)`,
	`CREATE TABLE sessions (
		id_hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		user_agent TEXT NOT NULL DEFAULT '')`,
	`CREATE INDEX sessions_user ON sessions(user_id)`,
	`CREATE TABLE passkeys (
		credential_id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		credential_json TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL,
		last_used_at TIMESTAMP NULL)`,
	`CREATE INDEX passkeys_user ON passkeys(user_id)`,
	`CREATE TABLE device_owners (
		device_id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TIMESTAMP NOT NULL)`,
	`CREATE INDEX device_owners_user ON device_owners(user_id)`,
	`CREATE TABLE settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_by TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMP NOT NULL)`,
	`CREATE TABLE audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL DEFAULT '',
		action TEXT NOT NULL,
		device_id TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL DEFAULT '',
		at TIMESTAMP NOT NULL)`,
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS wtf_schema (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT version FROM wtf_schema`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO wtf_schema(version) VALUES (0)`); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE wtf_schema SET version = ?`, i+1); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
