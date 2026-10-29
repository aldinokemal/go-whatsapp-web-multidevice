package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const versionHelperEnv = "GOWA_VERSION_HELPER_PROCESS"

// TestVersionHelperProcess is not a real test: TestVersionCommand re-executes
// the test binary into it so the CLI runs in its own process, exactly as the
// real binary would, with its own stdout, stderr, exit code, and working dir.
func TestVersionHelperProcess(t *testing.T) {
	if os.Getenv(versionHelperEnv) != "1" {
		return
	}
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	rootCmd.SetArgs(args)
	Execute()
	os.Exit(0)
}

func runVersionHelper(t *testing.T, dir string, env []string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestVersionHelperProcess$", "--"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), versionHelperEnv+"=1"), env...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else {
		require.NoError(t, err)
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestVersionCommand(t *testing.T) {
	want := strings.TrimPrefix(config.AppVersion, "v")
	require.NotEmpty(t, want)
	require.False(t, strings.HasPrefix(want, "v"))

	tests := []struct {
		name    string
		args    []string
		env     []string
		dotEnv  string
		wantOut string
		wantErr string
		wantRC  int
	}{
		{
			name:    "prints only the version",
			args:    []string{"version"},
			wantOut: want + "\n",
		},
		{
			name:    "accepts global flags without side effects",
			args:    []string{"--debug=true", "version", "--port=9999", "--db-uri=file:storages/flag.db"},
			wantOut: want + "\n",
		},
		{
			name: "ignores .env and environment configuration",
			args: []string{"version"},
			env: []string{
				"APP_DEBUG=true",
				"APP_PORT=4321",
				"WHATSAPP_WEBHOOK_SECRET=env-secret-should-not-print",
			},
			dotEnv:  "APP_DEBUG=true\nWHATSAPP_WEBHOOK_SECRET=dotenv-secret-should-not-print\n",
			wantOut: want + "\n",
		},
		{
			name:    "rejects positional arguments",
			args:    []string{"version", "extra"},
			wantErr: `unknown command "extra"`,
			wantRC:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			wantEntries := []string{}
			if tt.dotEnv != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(tt.dotEnv), 0o600))
				wantEntries = append(wantEntries, ".env")
			}

			stdout, stderr, rc := runVersionHelper(t, dir, tt.env, tt.args...)

			assert.Equal(t, tt.wantRC, rc, "stderr: %s", stderr)
			if tt.wantErr == "" {
				assert.Equal(t, tt.wantOut, stdout)
				assert.Empty(t, stderr)
			} else {
				assert.Empty(t, stdout)
				assert.Contains(t, stderr, tt.wantErr)
			}
			// initApp would create storages/ and statics/ here.
			assert.ElementsMatch(t, wantEntries, dirEntries(t, dir))
		})
	}
}
