//go:build purego
package sqlite

import _ "modernc.org/sqlite"

const DriverName = "sqlite"

// FormatChatStorageURI formats the URI for chat storage using modernc pragmas
func FormatChatStorageURI(baseURI string, enableWAL bool, enableFK bool) string {
	uri := baseURI
	if enableWAL {
		uri += "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(30000)&_pragma=synchronous(1)"
	}
	if enableFK {
		uri += "&_pragma=foreign_keys(1)"
	}
	return uri
}
