package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/require"
)

func TestChatwootEventsConfigPrecedence(t *testing.T) {
	if os.Getenv("GOWA_CHATWOOT_EVENTS_HELPER") == "1" {
		if value := os.Getenv("GOWA_CHATWOOT_EVENTS_FLAG"); value != "" {
			require.NoError(t, rootCmd.ParseFlags([]string{"--chatwoot-events=" + value}))
		}
		initEnvConfig()
		require.Equal(t, os.Getenv("GOWA_CHATWOOT_EVENTS_WANT"), strings.Join(config.ChatwootEvents, ","))
		return
	}

	for _, tc := range []struct{ name, env, flag, want string }{
		{"unset", "", "", ""},
		{"environment", "message, message.ack", "", "message,message.ack"},
		{"CLI without environment", "", "message", "message"},
		{"CLI overrides environment", "device.logged_out", "message", "message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestChatwootEventsConfigPrecedence$")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(),
				"GOWA_CHATWOOT_EVENTS_HELPER=1",
				"CHATWOOT_EVENTS="+tc.env,
				"GOWA_CHATWOOT_EVENTS_FLAG="+tc.flag,
				"GOWA_CHATWOOT_EVENTS_WANT="+tc.want)
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}
