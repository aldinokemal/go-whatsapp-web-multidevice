package rest

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInjectBetaCallConsole(t *testing.T) {
	t.Run("inserts before closing body", func(t *testing.T) {
		html := []byte("<!doctype html><html><body><div id=root></div></body></html>")
		injected := InjectBetaCallConsole(html, "/gowa")

		require.Contains(t, string(injected), `var base = "/gowa";`)
		require.Contains(t, string(injected), betaCallConsoleMarker)
		require.Less(t,
			strings.Index(string(injected), betaCallConsoleMarker),
			strings.Index(string(injected), "</body>"),
		)
		require.True(t, strings.HasSuffix(string(injected), "</body></html>"))
	})

	t.Run("appends when there is no closing body", func(t *testing.T) {
		injected := InjectBetaCallConsole([]byte("<div>fragment</div>"), "")

		require.True(t, strings.HasPrefix(string(injected), "<div>fragment</div>"))
		require.Contains(t, string(injected), `var base = "";`)
	})

	t.Run("is idempotent", func(t *testing.T) {
		once := InjectBetaCallConsole([]byte("<body></body>"), "")
		twice := InjectBetaCallConsole(once, "")

		require.Equal(t, once, twice)
		require.Equal(t, 1, bytes.Count(twice, []byte(betaCallConsoleMarker)))
	})

	t.Run("escapes the base path for the script context", func(t *testing.T) {
		injected := InjectBetaCallConsole(
			[]byte("<body></body>"),
			`</script><script>alert(1)</script>`,
		)

		require.NotContains(t, string(injected), "</script><script>alert(1)</script>")
	})

	t.Run("listens for incoming call broadcasts", func(t *testing.T) {
		injected := string(InjectBetaCallConsole([]byte("<body></body>"), ""))

		require.Contains(t, injected, "CALL_INCOMING")
		require.Contains(t, injected, "gowa-beta-call-banner")
		require.Contains(t, injected, `base + '/ws'`)
	})
}
