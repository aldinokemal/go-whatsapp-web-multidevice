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

// ErrInvalid is returned for a value outside what the store accepts.
var ErrInvalid = errors.New("invalid value")

// ErrPhoneTaken is returned when another user already has that phone number.
var ErrPhoneTaken = errors.New("phone number already in use")

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

// now is the only way store code reads the clock. Timestamps are compared as
// text in SQL, so they must always be UTC regardless of what Now returns.
func (s *Store) now() time.Time { return s.Now().UTC() }

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
	// Gap A/B: what a slot last held, and why its last link was refused.
	`ALTER TABLE device_owners ADD COLUMN last_phone TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE device_owners ADD COLUMN last_name TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE device_owners ADD COLUMN signed_out_at TIMESTAMP NULL`,
	`ALTER TABLE device_owners ADD COLUMN last_refusal TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE device_owners ADD COLUMN last_refusal_at TIMESTAMP NULL`,
	// Gap D: when a session was last used.
	`ALTER TABLE sessions ADD COLUMN last_seen_at TIMESTAMP NULL`,
	// Gap E: who did it (when not the subject) and a short non-secret detail.
	`ALTER TABLE audit_log ADD COLUMN actor_id TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE audit_log ADD COLUMN detail TEXT NOT NULL DEFAULT ''`,
	`CREATE INDEX audit_log_user ON audit_log(user_id)`,
	`CREATE INDEX audit_log_actor ON audit_log(actor_id)`,
	// Phase 2: personal API keys. Only the hash of a key is kept.
	`CREATE TABLE api_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key_hash TEXT NOT NULL UNIQUE,
		prefix TEXT NOT NULL,
		name TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		last_used_at TIMESTAMP NULL,
		revoked_at TIMESTAMP NULL)`,
	`CREATE INDEX api_keys_user ON api_keys(user_id)`,
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
