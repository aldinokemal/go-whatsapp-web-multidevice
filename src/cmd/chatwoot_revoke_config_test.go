package cmd

import (
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/require"
)

func TestChatwootRevokeOnDeleteConfigPrecedence(t *testing.T) {
	if os.Getenv("GOWA_REVOKE_CONFIG_HELPER") == "1" {
		original := config.ChatwootRevokeOnDelete
		flag := rootCmd.PersistentFlags().Lookup("chatwoot-revoke-on-delete")
		originalChanged := flag.Changed
		t.Cleanup(func() {
			config.ChatwootRevokeOnDelete = original
			flag.Changed = originalChanged
		})
		if value := os.Getenv("GOWA_REVOKE_TEST_FLAG"); value != "" {
			require.NoError(t, rootCmd.ParseFlags([]string{"--chatwoot-revoke-on-delete=" + value}))
		}
		initEnvConfig()
		want, err := strconv.ParseBool(os.Getenv("GOWA_REVOKE_TEST_WANT"))
		require.NoError(t, err)
		require.Equal(t, want, config.ChatwootRevokeOnDelete)
		return
	}

	for _, tc := range []struct {
		name, env, flag string
		want            bool
	}{
		{"default disabled", "", "", false},
		{"environment enabled", "true", "", true},
		{"environment disabled", "false", "", false},
		{"CLI enabled without environment", "", "true", true},
		{"CLI disabled without environment", "", "false", false},
		{"explicit false overrides environment true", "true", "false", false},
		{"explicit true overrides environment false", "false", "true", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestChatwootRevokeOnDeleteConfigPrecedence$")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(),
				"GOWA_REVOKE_CONFIG_HELPER=1",
				"CHATWOOT_REVOKE_ON_DELETE="+tc.env,
				"GOWA_REVOKE_TEST_FLAG="+tc.flag,
				"GOWA_REVOKE_TEST_WANT="+strconv.FormatBool(tc.want))
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}
