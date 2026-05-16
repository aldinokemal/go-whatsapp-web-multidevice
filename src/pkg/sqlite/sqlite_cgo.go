//go:build !purego

package sqlite

import (
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

const DriverName = "sqlite3"

// FormatChatStorageURI formats the URI for chat storage using standard SQLite syntax
func FormatChatStorageURI(baseURI string, enableWAL bool, enableFK bool) string {
	u, err := url.Parse(baseURI)
	if err != nil {
		return baseURI
	}

	q := u.Query()
	if enableWAL {
		q.Set("_journal_mode", "WAL")
		q.Set("_busy_timeout", "5000")
	}
	if enableFK {
		q.Set("_foreign_keys", "on")
	}
	u.RawQuery = q.Encode()
	return u.String()
}
