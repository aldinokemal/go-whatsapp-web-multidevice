package store

import (
	"context"
	"strings"
)

// UsersQuery is one page of the admin user list. Every field is checked
// here, so a value straight from a query string is safe to pass.
type UsersQuery struct {
	Search string // phone digits; anything else is stripped
	Status string // "", StatusActive or StatusDisabled; anything else means all
	Sort   string // "created", "last_login" or "phone"; empty or unknown means newest first
	Desc   bool
	Offset int
	Limit  int // 1 to MaxPageSize; 0 means DefaultPageSize
}

const (
	DefaultPageSize = 50
	MaxPageSize     = 100
)

// userSorts is the sort allowlist: the only text that ever reaches ORDER BY.
// Never-signed-in users sort last in both directions.
var userSorts = map[string][2]string{
	"created":    {"created_at ASC, id ASC", "created_at DESC, id DESC"},
	"last_login": {"last_login_at IS NULL, last_login_at ASC, id ASC", "last_login_at IS NULL, last_login_at DESC, id DESC"},
	"phone":      {"phone ASC", "phone DESC"},
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ListUsersPage filters, sorts and pages in SQL. total is the number of
// users matching the filters, not the length of the page.
func (s *Store) ListUsersPage(ctx context.Context, q UsersQuery) (total int, items []User, err error) {
	items = []User{}
	var where []string
	var args []any
	if strings.TrimSpace(q.Search) != "" {
		d := digitsOnly(q.Search)
		if d == "" {
			return 0, items, nil // nothing a phone number could match
		}
		where = append(where, "phone LIKE ?")
		args = append(args, "%"+d+"%") // digits only: no wildcard can get in
	}
	if q.Status == StatusActive || q.Status == StatusDisabled {
		where = append(where, "status = ?")
		args = append(args, q.Status)
	}
	cond := ""
	if len(where) > 0 {
		cond = " WHERE " + strings.Join(where, " AND ")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`+cond, args...).Scan(&total); err != nil {
		return 0, nil, err
	}
	by := userSorts["created"][1] // empty or unknown: newest first
	if order, ok := userSorts[q.Sort]; ok {
		by = order[0]
		if q.Desc {
			by = order[1]
		}
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users`+cond+` ORDER BY `+by+` LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return 0, nil, err
		}
		items = append(items, *u)
	}
	return total, items, rows.Err()
}

// DigitsOnly keeps only the digits 0-9 of s.
func DigitsOnly(s string) string { return digitsOnly(s) }
