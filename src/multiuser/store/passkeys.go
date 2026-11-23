package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Passkey struct {
	CredentialID   string // base64url of the WebAuthn credential id
	UserID         string
	Name           string
	CredentialJSON []byte // json of webauthn.Credential
	CreatedAt      time.Time
	LastUsedAt     *time.Time
}

const passkeyCols = `credential_id, user_id, name, credential_json, created_at, last_used_at`

func scanPasskey(row interface{ Scan(...any) error }) (*Passkey, error) {
	var p Passkey
	var js string
	var last sql.NullTime
	if err := row.Scan(&p.CredentialID, &p.UserID, &p.Name, &js, &p.CreatedAt, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.CredentialJSON = []byte(js)
	if last.Valid {
		t := last.Time
		p.LastUsedAt = &t
	}
	return &p, nil
}

func (s *Store) AddPasskey(ctx context.Context, p Passkey) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO passkeys (credential_id, user_id, name, credential_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		p.CredentialID, p.UserID, p.Name, string(p.CredentialJSON), s.now())
	return err
}

func (s *Store) PasskeysOf(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+passkeyCols+` FROM passkeys WHERE user_id = ? ORDER BY created_at, credential_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		p, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) PasskeyByCredentialID(ctx context.Context, credID string) (*Passkey, error) {
	return scanPasskey(s.db.QueryRowContext(ctx, `SELECT `+passkeyCols+` FROM passkeys WHERE credential_id = ?`, credID))
}

func (s *Store) UpdatePasskeyCredential(ctx context.Context, credID string, credJSON []byte) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE passkeys SET credential_json = ?, last_used_at = ? WHERE credential_id = ?`, string(credJSON), s.now(), credID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePasskey(ctx context.Context, userID, credID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE credential_id = ? AND user_id = ?`, credID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
