package cmd

import (
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFullHistoryConfig(t *testing.T) {
	viper.AutomaticEnv()

	for _, tc := range []struct {
		name, sync, days, size, args string
		wantSync                     bool
		wantDays, wantSize           uint32
		wantErr                      string
	}{
		{name: "defaults", wantDays: 3650, wantSize: 10240},
		{name: "environment", sync: "true", days: "365", size: "2048", wantSync: true, wantDays: 365, wantSize: 2048},
		{name: "explicit flags override environment", sync: "false", days: "365",
			args: "--full-history-sync=true --full-history-days=30", wantSync: true, wantDays: 30, wantSize: 10240},
		{name: "explicit false overrides environment true", sync: "true", args: "--full-history-sync=false",
			wantDays: 3650, wantSize: 10240},
		{name: "malformed days", days: "30d", wantErr: `WHATSAPP_FULL_HISTORY_DAYS must be a whole number of at least 1, got "30d"`},
		{name: "zero size", size: "0", wantErr: `WHATSAPP_FULL_HISTORY_SIZE_MB must be a whole number of at least 1, got "0"`},
		{name: "zero days flag", args: "--full-history-days=0", wantErr: "--full-history-days must be at least 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sync, days, size := config.WhatsappFullHistorySync, config.WhatsappFullHistoryDays, config.WhatsappFullHistorySizeMB
			t.Cleanup(func() {
				config.WhatsappFullHistorySync, config.WhatsappFullHistoryDays, config.WhatsappFullHistorySizeMB = sync, days, size
			})
			// An empty value counts as unset, as for the other settings.
			t.Setenv("WHATSAPP_FULL_HISTORY_SYNC", tc.sync)
			t.Setenv("WHATSAPP_FULL_HISTORY_DAYS", tc.days)
			t.Setenv("WHATSAPP_FULL_HISTORY_SIZE_MB", tc.size)

			// The same flags as rootCmd, on a command of the test's own, so a
			// parsed flag does not stay marked as changed for other tests.
			cmd := &cobra.Command{}
			cmd.PersistentFlags().BoolVar(&config.WhatsappFullHistorySync, "full-history-sync", config.WhatsappFullHistorySync, "")
			cmd.PersistentFlags().Uint32Var(&config.WhatsappFullHistoryDays, "full-history-days", config.WhatsappFullHistoryDays, "")
			require.NoError(t, cmd.ParseFlags(strings.Fields(tc.args)))

			err := loadFullHistoryConfig(cmd)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantSync, config.WhatsappFullHistorySync)
			assert.Equal(t, tc.wantDays, config.WhatsappFullHistoryDays)
			assert.Equal(t, tc.wantSize, config.WhatsappFullHistorySizeMB)
		})
	}
}
