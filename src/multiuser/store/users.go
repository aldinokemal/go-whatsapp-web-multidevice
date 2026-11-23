package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	// StatusDeleting marks an account whose deletion has started. Every
	// sign-in check refuses anything but StatusActive, so from this status on
	// no session, key or token works. It is one way: see SetUserStatus.
	StatusDeleting = "deleting"
)

// ErrDeleting is returned when a status change would bring back an account
// whose deletion has started.
var ErrDeleting = errors.New("account is being deleted")

type User struct {
	ID          string
	Phone       string
	Status      string
	MaxDevices  int
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

const userCols = `id, phone, status, max_devices, created_at, last_login_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var last sql.NullTime
	if err := row.Scan(&u.ID, &u.Phone, &u.Status, &u.MaxDevices, &u.CreatedAt, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if last.Valid {
		t := last.Time
		u.LastLoginAt = &t
	}
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, phone string) (*User, error) {
	u := &User{ID: newID("u_"), Phone: phone, Status: StatusActive, MaxDevices: 2, CreatedAt: s.now()}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, phone, status, max_devices, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Phone, u.Status, u.MaxDevices, u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

func (s *Store) UserByPhone(ctx context.Context, phone string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE phone = ?`, phone))
}

func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

// ListUsers returns every user, newest first. Phase 1 has few users; phase 4
// adds paging when sign-up opens.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// SetUserStatus changes a user's status. An account in StatusDeleting stays
// there (ErrDeleting): its deletion must finish, not be undone half way.
func (s *Store) SetUserStatus(ctx context.Context, id, status string) error {
	if status != StatusActive && status != StatusDisabled && status != StatusDeleting {
		return fmt.Errorf("unknown status %q", status)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET status = ? WHERE id = ? AND (status != ? OR ? = ?)`,
		status, id, StatusDeleting, status, StatusDeleting)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		u, err := s.UserByID(ctx, id)
		if err != nil {
			return err
		}
		if u.Status == StatusDeleting {
			return ErrDeleting
		}
		return ErrNotFound
	}
	return nil
}

// UsersWithStatus lists every user in the given status, oldest first.
func (s *Store) UsersWithStatus(ctx context.Context, status string) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users WHERE status = ? ORDER BY created_at, id`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// DeleteUser deletes the user row. Foreign keys (on for this database; see
// Open) cascade to the user's sessions, passkeys, device owners and API keys.
// The audit log has no foreign key and is kept. ErrNotFound if there is no
// such user. Callers (account deletion) do everything else first.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) TouchLogin(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, s.now(), id)
	return err
}

// MaxDevicesLimit is the most numbers an admin may let one user link.
const MaxDevicesLimit = 20

// SetMaxDevices sets how many numbers the user may link (0 to MaxDevicesLimit).
// Lowering it below what they already have unlinks nothing; they just cannot
// add more.
func (s *Store) SetMaxDevices(ctx context.Context, id string, n int) error {
	if n < 0 || n > MaxDevicesLimit {
		return ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET max_devices = ? WHERE id = ?`, n, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserPhone changes the phone number the user signs in with and returns
// the old one. phone must already be normalised (NormalizePhone).
// ErrPhoneTaken when another user has it. Unused sign-in links minted for the
// old number are deleted in the same transaction, so none can sign in (or sign
// up) as the old number afterwards.
func (s *Store) SetUserPhone(ctx context.Context, id, phone string) (old string, err error) {
	if norm, err := NormalizePhone(phone); err != nil || norm != phone {
		return "", ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT phone FROM users WHERE id = ?`, id).Scan(&old); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET phone = ? WHERE id = ?`, phone, id); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "", ErrPhoneTaken
		}
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM login_tokens WHERE phone = ? AND used_at IS NULL`, old); err != nil {
		return "", err
	}
	return old, tx.Commit()
}

// UsersByID looks up several users at once; unknown ids are left out.
func (s *Store) UsersByID(ctx context.Context, ids []string) (map[string]User, error) {
	out := map[string]User{}
	var args []any
	var marks []string
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		args = append(args, id)
		marks = append(marks, "?")
	}
	if len(args) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users WHERE id IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out[u.ID] = *u
	}
	return out, rows.Err()
}
