package store

import (
	"context"
	"database/sql"
	"errors"
)

// ActionAccountDeleted is the audit action EraseUser writes. Its line names
// the user id only: no number, no IP, no detail.
const ActionAccountDeleted = "account_deleted"

// DeleteLoginTokensFor deletes every sign-in link minted for phone, used or
// not. Links are keyed by number, not user, so they do not cascade with the
// user row. An empty phone deletes nothing.
func (s *Store) DeleteLoginTokensFor(ctx context.Context, phone string) error {
	if phone == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM login_tokens WHERE phone = ?`, phone)
	return err
}

// EraseUser is the last step of account deletion, in one transaction:
//   - the audit lines about the user lose their detail and IP (a detail can
//     hold a phone number), and the lines the user wrote as an admin lose
//     their IP (their detail is about someone else, so it stays);
//   - one account_deleted line is written, naming the user id only;
//   - the user row is deleted, and foreign keys cascade to sessions,
//     passkeys, device owners and API keys.
//
// The audit log itself is kept (it is never pruned). ErrNotFound, with
// nothing written, when there is no such user, so a repeat is harmless.
func (s *Store) EraseUser(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var found string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = ?`, id).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE audit_log SET detail = '', ip = '' WHERE user_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE audit_log SET ip = '' WHERE actor_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_log (user_id, actor_id, action, device_id, ip, detail, at) VALUES (?, '', ?, '', '', '', ?)`,
		id, ActionAccountDeleted, s.now()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
