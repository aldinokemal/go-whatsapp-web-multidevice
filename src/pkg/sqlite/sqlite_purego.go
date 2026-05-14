//go:build purego
package sqlite

import (
	"strings"

	_ "modernc.org/sqlite"
)

const DriverName = "sqlite"

// FormatChatStorageURI formats the URI for chat storage using modernc pragmas
func FormatChatStorageURI(baseURI string, enableWAL bool, enableFK bool) string {
	var pragmaParams []string
	if enableWAL {
		pragmaParams = append(pragmaParams, "_pragma=journal_mode(WAL)", "_pragma=busy_timeout(30000)", "_pragma=synchronous(1)")
	}
	if enableFK {
		pragmaParams = append(pragmaParams, "_pragma=foreign_keys(1)")
	}

	if len(pragmaParams) == 0 {
		return baseURI
	}

	delimiter := "?"
	if strings.Contains(baseURI, "?") {
		delimiter = "&"
	}
	return baseURI + delimiter + strings.Join(pragmaParams, "&")
}
