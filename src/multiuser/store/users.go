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
)

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
	return createUser(ctx, s.db, phone, 2, s.now()) // 2 is the column default
}

// CreateUserWithLimit creates a user who may link up to maxDevices numbers
// (1 to MaxDevicesLimit).
func (s *Store) CreateUserWithLimit(ctx context.Context, phone string, maxDevices int) (*User, error) {
	if maxDevices < 1 || maxDevices > MaxDevicesLimit {
		return nil, ErrInvalid
	}
	return createUser(ctx, s.db, phone, maxDevices, s.now())
}

func createUser(ctx context.Context, db execer, phone string, maxDevices int, now time.Time) (*User, error) {
	u := &User{ID: newID("u_"), Phone: phone, Status: StatusActive, MaxDevices: maxDevices, CreatedAt: now}
	_, err := db.ExecContext(ctx,
		`INSERT INTO users (id, phone, status, max_devices, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Phone, u.Status, u.MaxDevices, u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
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

func (s *Store) SetUserStatus(ctx context.Context, id, status string) error {
	if status != StatusActive && status != StatusDisabled {
		return fmt.Errorf("unknown status %q", status)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET status = ? WHERE id = ?`, status, id)
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
