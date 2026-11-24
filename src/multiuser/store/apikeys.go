package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// APIKeyPrefix starts every personal API key, so a key is recognisable in a
// header, a log scanner or a leaked file.
const APIKeyPrefix = "wtfk_"

// MaxAPIKeysPerUser is how many live keys one person may hold.
const MaxAPIKeysPerUser = 10

// MaxAPIKeyNameLen is the longest key name, in characters.
const MaxAPIKeyNameLen = 60

// apiKeyTouchEvery throttles last-used writes to one per key per interval.
const apiKeyTouchEvery = 5 * time.Minute

// ErrTooManyKeys is returned when the user already holds MaxAPIKeysPerUser live keys.
var ErrTooManyKeys = errors.New("too many API keys")

// APIKey is a key as listed; the raw key exists only in CreateAPIKey's result.
type APIKey struct {
	ID         string
	UserID     string
	Prefix     string // the first 6 characters after APIKeyPrefix, for display
	Name       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreateAPIKey makes a key for userID and returns the raw key, which is never
// stored or shown again. The live-key count and the insert are one statement,
// so concurrent creates cannot take a user past MaxAPIKeysPerUser.
func (s *Store) CreateAPIKey(ctx context.Context, userID, name string) (string, *APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > MaxAPIKeyNameLen {
		return "", nil, ErrInvalid
	}
	raw := APIKeyPrefix + NewToken()
	k := &APIKey{
		ID:        newID("k_"),
		UserID:    userID,
		Prefix:    raw[len(APIKeyPrefix) : len(APIKeyPrefix)+6],
		Name:      name,
		CreatedAt: s.now(),
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, user_id, key_hash, prefix, name, created_at)
		 SELECT ?, ?, ?, ?, ?, ?
		 WHERE (SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND revoked_at IS NULL) < ?`,
		k.ID, userID, HashToken(raw), k.Prefix, k.Name, k.CreatedAt, userID, MaxAPIKeysPerUser)
	if err != nil {
		return "", nil, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return "", nil, err
	} else if n != 1 {
		return "", nil, ErrTooManyKeys
	}
	return raw, k, nil
}

// APIKeysOf lists the user's live keys, newest first.
func (s *Store) APIKeysOf(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, prefix, name, created_at, last_used_at FROM api_keys
		 WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		var last sql.NullTime
		if err := rows.Scan(&k.ID, &k.UserID, &k.Prefix, &k.Name, &k.CreatedAt, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			t := last.Time
			k.LastUsedAt = &t
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeAPIKey revokes one of the user's live keys. A key that is not the
// user's, or not live, is ErrNotFound: the same answer as a key that never was.
func (s *Store) RevokeAPIKey(ctx context.Context, userID, keyID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL`,
		s.now(), keyID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAllAPIKeys revokes every live key the user holds.
func (s *Store) RevokeAllAPIKeys(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, s.now(), userID)
	return err
}

// UserByAPIKey returns the owner of a live key and the key's id. Anything
// else (no prefix, unknown, revoked) is ErrNotFound. The user's status is the
// caller's to check: it is read fresh here on every call.
func (s *Store) UserByAPIKey(ctx context.Context, raw string) (*User, string, error) {
	if !strings.HasPrefix(raw, APIKeyPrefix) || len(raw) == len(APIKeyPrefix) {
		return nil, "", ErrNotFound
	}
	var keyID string
	row := s.db.QueryRowContext(ctx,
		`SELECT k.id, u.id, u.phone, u.status, u.max_devices, u.created_at, u.last_login_at
		 FROM api_keys k JOIN users u ON u.id = k.user_id
		 WHERE k.key_hash = ? AND k.revoked_at IS NULL`, HashToken(raw))
	var u User
	var last sql.NullTime
	err := row.Scan(&keyID, &u.ID, &u.Phone, &u.Status, &u.MaxDevices, &u.CreatedAt, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if last.Valid {
		t := last.Time
		u.LastLoginAt = &t
	}
	return &u, keyID, nil
}

// TouchAPIKey records that a key was used, at most once per key every five
// minutes, so a busy script does not write on every request.
func (s *Store) TouchAPIKey(ctx context.Context, keyID string) error {
	now := s.now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = ?
		 WHERE id = ? AND (last_used_at IS NULL OR last_used_at <= ?)`,
		now, keyID, now.Add(-apiKeyTouchEvery))
	return err
}
