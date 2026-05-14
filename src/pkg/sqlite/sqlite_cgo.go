//go:build !purego
package sqlite

import _ "github.com/mattn/go-sqlite3"

const DriverName = "sqlite3"

// FormatChatStorageURI formats the URI for chat storage using standard SQLite syntax
func FormatChatStorageURI(baseURI string, enableWAL bool, enableFK bool) string {
	uri := baseURI
	if enableWAL {
		uri += "?_journal_mode=WAL&_busy_timeout=5000"
	}
	if enableFK {
		uri += "&_foreign_keys=on"
	}
	return uri
}
