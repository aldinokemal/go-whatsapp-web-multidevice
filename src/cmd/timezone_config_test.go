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

func TestLoadTimezoneConfig(t *testing.T) {
	viper.AutomaticEnv()

	for _, tc := range []struct {
		name, env, args, want, wantErr string
	}{
		{name: "defaults to UTC", want: "UTC"},
		{name: "environment", env: "Europe/Paris", want: "Europe/Paris"},
		{name: "flag overrides environment", env: "Europe/Paris", args: "--timezone=Asia/Jakarta", want: "Asia/Jakarta"},
		{name: "unknown zone", env: "Mars/Olympus", wantErr: `invalid timezone "Mars/Olympus" (APP_TIMEZONE or --timezone): use an IANA name such as Europe/Paris`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			timezone, location := config.AppTimezone, config.AppLocation
			t.Cleanup(func() { config.AppTimezone, config.AppLocation = timezone, location })
			// An empty value counts as unset, as for the other settings.
			t.Setenv("APP_TIMEZONE", tc.env)

			// The same flag as rootCmd, on a command of the test's own, so a
			// parsed flag does not stay marked as changed for other tests.
			cmd := &cobra.Command{}
			cmd.PersistentFlags().StringVar(&config.AppTimezone, "timezone", config.AppTimezone, "")
			require.NoError(t, cmd.ParseFlags(strings.Fields(tc.args)))

			err := loadTimezoneConfig(cmd)
			if tc.wantErr != "" {
				require.EqualError(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, config.AppLocation.String())
		})
	}
}
