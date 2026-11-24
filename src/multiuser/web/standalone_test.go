package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanOriginRefusesAnythingButAnOrigin(t *testing.T) {
	for _, ok := range []string{"https://claude.ai", "http://localhost:54321", "http://127.0.0.1:9", "http://[::1]:8080"} {
		assert.Equal(t, ok, cleanOrigin(ok), ok)
	}
	for _, bad := range []string{"", "https://claude.ai/", "https://claude.ai/cb", "https://claude.ai?x", "javascript:alert(1)",
		"https://a.example https://b.example", "https://a.example;script-src *", "'unsafe-inline'", "ftp://x.example",
		"https://user@claude.ai", "https://claude.ai#f", "data:", "*"} {
		assert.Empty(t, cleanOrigin(bad), bad)
	}
}
