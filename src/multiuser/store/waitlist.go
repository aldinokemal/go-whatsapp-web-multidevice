package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Waitlist statuses.
const (
	WaitlistWaiting  = "waiting"
	WaitlistAdmitted = "admitted"
	WaitlistDeclined = "declined"
)

// WaitlistEntry is one person who asked the service number for an account.
// It is not an account: it cannot sign in and holds no capacity.
type WaitlistEntry struct {
	Phone       string
	Status      string
	CreatedAt   time.Time
	LastAskedAt time.Time
	DecidedAt   *time.Time
	DecidedBy   string
}

func validWaitlistStatus(s string) bool {
	return s == WaitlistWaiting || s == WaitlistAdmitted || s == WaitlistDeclined
}

// isPhone accepts only an already-normalised E.164 number, so nothing else
// (a LID, a raw JID, free text) can ever land on the waitlist.
func isPhone(phone string) bool {
	norm, err := NormalizePhone(phone)
	return err == nil && norm == phone
}

// JoinWaitlist puts phone on the waitlist, or bumps last_asked_at when it is
// already there, so repeated asks never add rows. A declined number stays
// declined (joined=false, position 0). An admitted row whose account has
// since gone starts waiting again. position counts waiting rows ahead of
// this one, plus one.
//
// The upsert is the transaction's first statement, so it takes the write
// lock before anything is read and the position is read in the same view.
func (s *Store) JoinWaitlist(ctx context.Context, phone string) (joined bool, position int, err error) {
	if !isPhone(phone) {
		return false, 0, ErrInvalid
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO waitlist (phone, status, created_at, last_asked_at) VALUES (?, 'waiting', ?, ?)
		 ON CONFLICT(phone) DO UPDATE SET
		   last_asked_at = excluded.last_asked_at,
		   created_at = CASE WHEN status = 'admitted' THEN excluded.created_at ELSE created_at END,
		   decided_at = CASE WHEN status = 'admitted' THEN NULL ELSE decided_at END,
		   decided_by = CASE WHEN status = 'admitted' THEN '' ELSE decided_by END,
		   status = CASE WHEN status = 'admitted' THEN 'waiting' ELSE status END`,
		phone, now, now); err != nil {
		return false, 0, err
	}
	var status string
	var created time.Time
	if err := tx.QueryRowContext(ctx, `SELECT status, created_at FROM waitlist WHERE phone = ?`, phone).Scan(&status, &created); err != nil {
		return false, 0, err
	}
	if status != WaitlistWaiting {
		return false, 0, tx.Commit()
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) + 1 FROM waitlist WHERE status = 'waiting'
		 AND (created_at < ? OR (created_at = ? AND phone < ?))`,
		created, created, phone).Scan(&position); err != nil {
		return false, 0, err
	}
	return true, position, tx.Commit()
}

// WaitlistPage lists one status, oldest first. total counts every row with
// that status, not the page.
func (s *Store) WaitlistPage(ctx context.Context, status string, offset, limit int) (total int, items []WaitlistEntry, err error) {
	if !validWaitlistStatus(status) {
		return 0, nil, ErrInvalid
	}
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	if total, err = s.WaitlistCount(ctx, status); err != nil {
		return 0, nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT phone, status, created_at, last_asked_at, decided_at, decided_by FROM waitlist
		 WHERE status = ? ORDER BY created_at, phone LIMIT ? OFFSET ?`, status, limit, offset)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	items = []WaitlistEntry{}
	for rows.Next() {
		var e WaitlistEntry
		var decided sql.NullTime
		if err := rows.Scan(&e.Phone, &e.Status, &e.CreatedAt, &e.LastAskedAt, &decided, &e.DecidedBy); err != nil {
			return 0, nil, err
		}
		e.DecidedAt = nullTimePtr(decided)
		items = append(items, e)
	}
	return total, items, rows.Err()
}

// WaitlistCount is how many rows have status.
func (s *Store) WaitlistCount(ctx context.Context, status string) (int, error) {
	if !validWaitlistStatus(status) {
		return 0, ErrInvalid
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM waitlist WHERE status = ?`, status).Scan(&n)
	return n, err
}

// AdmitFromWaitlist lets a waiting person in, in one transaction: the row is
// marked admitted and, unless an account already exists for the phone (they
// signed up meanwhile, or another admin let them in first), one is created
// with maxDevices. createdNow says which, so a caller sends the link at most
// once. ErrNotFound when the phone never asked; ErrInvalid when it was
// declined (move it back to waiting first).
//
// The first statement writes, so concurrent admits for one phone queue on
// SQLite's write lock and each later one finds the user the first created.
func (s *Store) AdmitFromWaitlist(ctx context.Context, phone, by string, maxDevices int) (*User, bool, error) {
	if maxDevices < 1 || maxDevices > MaxDevicesLimit {
		return nil, false, ErrInvalid
	}
	now := s.now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE waitlist SET status = 'admitted', decided_at = ?, decided_by = ? WHERE phone = ? AND status = 'waiting'`,
		now, by, phone); err != nil {
		return nil, false, err
	}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM waitlist WHERE phone = ?`, phone).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}
	if status != WaitlistAdmitted {
		return nil, false, ErrInvalid
	}
	u, err := scanUser(tx.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE phone = ?`, phone))
	created := false
	switch {
	case errors.Is(err, ErrNotFound):
		if u, err = createUser(ctx, tx, phone, maxDevices, now); err != nil {
			return nil, false, err
		}
		created = true
	case err != nil:
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return u, created, nil
}

// DeclineWaitlist marks a waiting number declined. They are not told. Doing
// it twice is fine; ErrNotFound when the phone is not on the list,
// ErrInvalid when it was already let in.
func (s *Store) DeclineWaitlist(ctx context.Context, phone, by string) error {
	return s.decide(ctx, phone, WaitlistWaiting, WaitlistDeclined, by)
}

// ReopenWaitlist moves a declined number back to waiting, keeping its place.
// No message is sent.
func (s *Store) ReopenWaitlist(ctx context.Context, phone string) error {
	return s.decide(ctx, phone, WaitlistDeclined, WaitlistWaiting, "")
}

func (s *Store) decide(ctx context.Context, phone, from, to, by string) error {
	var decided any
	if to != WaitlistWaiting {
		decided = s.now()
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE waitlist SET status = ?, decided_at = ?, decided_by = ? WHERE phone = ? AND status = ?`,
		to, decided, by, phone, from)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	var status string
	err = s.db.QueryRowContext(ctx, `SELECT status FROM waitlist WHERE phone = ?`, phone).Scan(&status)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return err
	case status == to:
		return nil
	default:
		return ErrInvalid
	}
}

// RemoveFromWaitlist deletes the phone's row, if any. Account deletion calls
// it so a deleted person leaves nothing behind.
func (s *Store) RemoveFromWaitlist(ctx context.Context, phone string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM waitlist WHERE phone = ?`, phone)
	return err
}
