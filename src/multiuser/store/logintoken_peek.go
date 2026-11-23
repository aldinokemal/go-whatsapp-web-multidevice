package store

import (
	"context"
	"database/sql"
	"errors"
)

// LoginTokenPhone returns the phone a live (unused, unexpired) sign-in token
// was minted for, without consuming it, so the confirm page can tell a
// disabled account apart from an expired link. ok is false for a dead token.
func (s *Store) LoginTokenPhone(ctx context.Context, raw string) (phone string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT phone FROM login_tokens WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		HashToken(raw), s.now()).Scan(&phone)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return phone, err == nil, err
}
