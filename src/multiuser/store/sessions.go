package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Session is the stored shape of a login session (the raw id is never kept).
type Session struct {
	UserID    string
	ExpiresAt time.Time
}

func (s *Store) CreateSession(ctx context.Context, userID, userAgent string, ttl time.Duration) (string, error) {
	raw := NewToken()
	now := s.now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id_hash, user_id, created_at, expires_at, user_agent) VALUES (?, ?, ?, ?, ?)`,
		HashToken(raw), userID, now, now.Add(ttl), userAgent)
	if err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Store) SessionUser(ctx context.Context, raw string, ttl time.Duration) (*User, error) {
	h := HashToken(raw)
	var expires time.Time
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id, expires_at FROM sessions WHERE id_hash = ?`, h).Scan(&userID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("session lookup: %w", err)
	}
	now := s.now()
	if !expires.After(now) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, h)
		return nil, ErrNotFound
	}
	if expires.Sub(now) < ttl/2 {
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE id_hash = ?`, now.Add(ttl), h)
	}
	return s.UserByID(ctx, userID)
}

func (s *Store) DeleteSession(ctx context.Context, raw string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, HashToken(raw))
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}
