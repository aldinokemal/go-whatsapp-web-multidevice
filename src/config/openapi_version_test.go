package config

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenAPIInfoVersionMatchesAppVersion keeps docs/openapi.yaml in step with
// AppVersion. The new-release skill runs `go test ./...` before committing a
// release, so a bump that skips the spec fails here instead of shipping.
func TestOpenAPIInfoVersionMatchesAppVersion(t *testing.T) {
	spec, err := os.ReadFile("../../docs/openapi.yaml")
	require.NoError(t, err)

	var specVersion string
	inInfo := false
	for _, line := range strings.Split(string(spec), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "info:" {
			inInfo = true
			continue
		}
		if !inInfo {
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") {
			break
		}
		if value, found := strings.CutPrefix(line, "  version:"); found {
			specVersion = strings.Trim(strings.TrimSpace(value), `"'`)
			break
		}
	}
	require.NotEmpty(t, specVersion, "info.version not found in docs/openapi.yaml")

	require.Equal(t, strings.TrimPrefix(AppVersion, "v"), specVersion,
		"update info.version in docs/openapi.yaml together with AppVersion")
}
