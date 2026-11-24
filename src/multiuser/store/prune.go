package store

import (
	"context"
	"time"
)

// usedTokenGrace keeps a just-used sign-in link for a moment, because
// ConsumeLoginToken reads its phone back right after marking it used.
const usedTokenGrace = time.Minute

// PruneCounts says how many dead rows PruneExpired removed.
type PruneCounts struct {
	Sessions    int64
	LoginTokens int64
}

// PruneExpired removes rows that can never be used again: sessions at or past
// their expiry (SessionUser already refuses them) and sign-in links that are
// used (a minute ago or more) or expired. It never touches users, passkeys, device owners, API keys,
// settings or the audit log: those are configuration and are kept.
func (s *Store) PruneExpired(ctx context.Context) (PruneCounts, error) {
	var c PruneCounts
	now := s.now()
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now)
	if err != nil {
		return c, err
	}
	c.Sessions, _ = res.RowsAffected()
	res, err = s.db.ExecContext(ctx, `DELETE FROM login_tokens WHERE used_at <= ? OR expires_at <= ?`, now.Add(-usedTokenGrace), now)
	if err != nil {
		return c, err
	}
	c.LoginTokens, _ = res.RowsAffected()
	return c, nil
}
