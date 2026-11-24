package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Every timestamp in accounts.db is stored as text (the driver formats a
// time.Time with whatever zone it carries), and the store compares and sorts
// those columns as text in SQL: expires_at > ?, created_at < ?, ORDER BY
// created_at. Text order is time order only when every value is in the same
// zone, so the store writes UTC only: its clock is s.now() (UTC), and a time
// handed in by a caller is converted with .UTC() before it is bound.
//
// migrateTimesToUTC is a migration entry run in Go: it rewrites any stored
// timestamp that is not UTC (from before the clock was pinned, or written by
// hand) to the same instant in UTC.
const migrateTimesToUTC = "-- go: rewrite stored timestamps to UTC"

// storedTimeLayouts are the forms a time.Time takes once written by a SQLite
// driver (mattn first; modernc writes time.Time.String) or by SQLite itself.
var storedTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999 -0700", // modernc, zone name cut off
	"2006-01-02 15:04:05.999999999Z07:00",
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
}

// parseStoredTime reads a stored timestamp. ok is false for text in no known
// form; such a value is left as it is.
func parseStoredTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " m="); i > 0 { // monotonic suffix from time.String
		s = s[:i]
	}
	if f := strings.Fields(s); len(f) == 4 && len(f[2]) == 5 && (f[2][0] == '+' || f[2][0] == '-') {
		s = f[0] + " " + f[1] + " " + f[2] // drop the free-text zone name
	}
	for _, layout := range storedTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// timesToUTC rewrites, in every table, every TIMESTAMP column value whose
// offset is not zero to the same instant in UTC.
func timesToUTC(ctx context.Context, tx *sql.Tx) error {
	cols, err := timestampColumns(ctx, tx)
	if err != nil {
		return err
	}
	for _, c := range cols {
		type fix struct {
			rowid int64
			t     time.Time
		}
		var fixes []fix
		rows, err := tx.QueryContext(ctx,
			`SELECT rowid, CAST("`+c.col+`" AS TEXT) FROM "`+c.table+`" WHERE "`+c.col+`" IS NOT NULL`)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", c.table, c.col, err)
		}
		for rows.Next() {
			var id int64
			var raw string
			if err := rows.Scan(&id, &raw); err != nil {
				_ = rows.Close()
				return err
			}
			if t, ok := parseStoredTime(raw); ok {
				if _, off := t.Zone(); off != 0 {
					fixes = append(fixes, fix{id, t.UTC()})
				}
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, f := range fixes {
			if _, err := tx.ExecContext(ctx, `UPDATE "`+c.table+`" SET "`+c.col+`" = ? WHERE rowid = ?`, f.t, f.rowid); err != nil {
				return fmt.Errorf("%s.%s: %w", c.table, c.col, err)
			}
		}
	}
	return nil
}

type tableColumn struct{ table, col string }

// timestampColumns lists every column declared TIMESTAMP in the database.
func timestampColumns(ctx context.Context, tx *sql.Tx) ([]tableColumn, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p
		 WHERE m.type = 'table' AND upper(p.type) = 'TIMESTAMP' ORDER BY m.name, p.cid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tableColumn
	for rows.Next() {
		var c tableColumn
		if err := rows.Scan(&c.table, &c.col); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
