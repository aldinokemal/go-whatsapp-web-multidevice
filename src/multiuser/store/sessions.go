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
	// Last seen is for the Account page; a minute's precision is plenty and
	// keeps most requests from writing.
	_, _ = s.db.ExecContext(ctx,
		`UPDATE sessions SET last_seen_at = ? WHERE id_hash = ? AND (last_seen_at IS NULL OR last_seen_at < ?)`,
		now, h, now.Add(-time.Minute))
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

// SessionInfo is one live session as the Account page lists it. IDHash is the
// stored hash, never the raw cookie value; it must not reach HTML as is.
type SessionInfo struct {
	IDHash     string
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt *time.Time
}

// SessionsOf lists the user's unexpired sessions, newest first.
func (s *Store) SessionsOf(ctx context.Context, userID string) ([]SessionInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id_hash, user_agent, created_at, last_seen_at FROM sessions
		 WHERE user_id = ? AND expires_at > ? ORDER BY created_at DESC, id_hash`, userID, s.now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var si SessionInfo
		var seen sql.NullTime
		if err := rows.Scan(&si.IDHash, &si.UserAgent, &si.CreatedAt, &seen); err != nil {
			return nil, err
		}
		si.LastSeenAt = nullTimePtr(seen)
		out = append(out, si)
	}
	return out, rows.Err()
}

// DeleteUserSession ends one of the user's own sessions by its stored hash.
// A hash that belongs to another user is ErrNotFound and nothing is deleted.
func (s *Store) DeleteUserSession(ctx context.Context, userID, idHash string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ? AND user_id = ?`, idHash, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
