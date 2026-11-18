package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/require"
)

func TestFullHistoryConfig(t *testing.T) {
	// Each process isolates Viper and the globals initEnvConfig changes, and
	// lets a fatal configuration error end the process.
	if os.Getenv("GOWA_FULL_HISTORY_HELPER") == "1" {
		if args := os.Getenv("GOWA_FULL_HISTORY_ARGS"); args != "" {
			require.NoError(t, rootCmd.ParseFlags(strings.Fields(args)))
		}
		initEnvConfig()
		fmt.Printf("RESULT sync=%v days=%d size=%d\n",
			config.WhatsappFullHistorySync, config.WhatsappFullHistoryDays, config.WhatsappFullHistorySizeMB)
		return
	}

	for _, tc := range []struct {
		name, sync, days, size, args string
		want                         string
		fails                        bool
	}{
		{name: "defaults", want: "RESULT sync=false days=3650 size=10240"},
		{name: "environment", sync: "true", days: "365", size: "2048", want: "RESULT sync=true days=365 size=2048"},
		{name: "explicit flags override environment", sync: "false", days: "365",
			args: "--full-history-sync=true --full-history-days=30", want: "RESULT sync=true days=30"},
		{name: "explicit false overrides environment true", sync: "true", args: "--full-history-sync=false", want: "RESULT sync=false"},
		{name: "malformed days", days: "30d", fails: true,
			want: `WHATSAPP_FULL_HISTORY_DAYS must be a whole number of at least 1, got \"30d\"`},
		{name: "zero size", size: "0", fails: true,
			want: `WHATSAPP_FULL_HISTORY_SIZE_MB must be a whole number of at least 1, got \"0\"`},
		{name: "zero days flag", args: "--full-history-days=0", fails: true, want: "--full-history-days must be at least 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestFullHistoryConfig$")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(),
				"GOWA_FULL_HISTORY_HELPER=1",
				"GOWA_FULL_HISTORY_ARGS="+tc.args,
				"WHATSAPP_FULL_HISTORY_SYNC="+tc.sync,
				"WHATSAPP_FULL_HISTORY_DAYS="+tc.days,
				"WHATSAPP_FULL_HISTORY_SIZE_MB="+tc.size)
			output, err := cmd.CombinedOutput()
			if tc.fails {
				require.Error(t, err, "%s", output)
			} else {
				require.NoError(t, err, "%s", output)
			}
			require.Contains(t, string(output), tc.want)
		})
	}
}
