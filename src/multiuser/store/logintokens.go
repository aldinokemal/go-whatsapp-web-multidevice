package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) CreateLoginToken(ctx context.Context, phone string, ttl time.Duration) (string, error) {
	raw := NewToken()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO login_tokens (token_hash, phone, expires_at) VALUES (?, ?, ?)`,
		HashToken(raw), phone, s.now().Add(ttl))
	if err != nil {
		return "", err
	}
	// Opportunistic cleanup of dead tokens.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM login_tokens WHERE expires_at < ?`, s.now().Add(-24*time.Hour))
	return raw, nil
}

func (s *Store) LoginTokenValid(ctx context.Context, raw string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM login_tokens WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		HashToken(raw), s.now()).Scan(&n)
	return n == 1, err
}

// ConsumeLoginToken marks the token used in the same statement that checks it,
// so two concurrent POSTs cannot both succeed.
func (s *Store) ConsumeLoginToken(ctx context.Context, raw string) (string, error) {
	h := HashToken(raw)
	res, err := s.db.ExecContext(ctx,
		`UPDATE login_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		s.now(), h, s.now())
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", ErrNotFound
	}
	var phone string
	if err := s.db.QueryRowContext(ctx, `SELECT phone FROM login_tokens WHERE token_hash = ?`, h).Scan(&phone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return phone, nil
}
