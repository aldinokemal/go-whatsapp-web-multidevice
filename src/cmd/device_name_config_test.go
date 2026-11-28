package cmd

import (
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestLoadDeviceNameConfig(t *testing.T) {
	viper.AutomaticEnv()

	for _, tc := range []struct {
		name, appendVersion, pairName string
		args                          []string
		wantAppendVersion             bool
		wantPairName                  string
	}{
		{name: "defaults", wantAppendVersion: true, wantPairName: "Chrome (Linux)"},
		{name: "environment", appendVersion: "false", pairName: "Safari (Mac OS)", wantPairName: "Safari (Mac OS)"},
		{name: "flags override environment", appendVersion: "false", pairName: "Safari (Mac OS)",
			args:              []string{"--os-append-version=true", "--pair-display-name=Firefox (Windows)"},
			wantAppendVersion: true, wantPairName: "Firefox (Windows)"},
		{name: "explicit flag false overrides environment true", appendVersion: "true",
			args: []string{"--os-append-version=false"}, wantPairName: "Chrome (Linux)"},
		// A malformed label does not stop startup; code pairing rejects it
		// when requested (see usecase.TestMalformedPairLabelOnlyBlocksCodePairing).
		{name: "malformed label from environment is kept", pairName: "GOWA", wantAppendVersion: true, wantPairName: "GOWA"},
		{name: "malformed label from flag is kept", args: []string{"--pair-display-name=Chrome(Linux)"},
			wantAppendVersion: true, wantPairName: "Chrome(Linux)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			appendVersion, pairName := config.AppOsAppendVersion, config.AppPairDisplayName
			t.Cleanup(func() { config.AppOsAppendVersion, config.AppPairDisplayName = appendVersion, pairName })
			// An empty value counts as unset, as for the other settings.
			t.Setenv("APP_OS_APPEND_VERSION", tc.appendVersion)
			t.Setenv("APP_PAIR_DISPLAY_NAME", tc.pairName)

			// The same flags as rootCmd, on a command of the test's own, so a
			// parsed flag does not stay marked as changed for other tests.
			cmd := &cobra.Command{}
			cmd.PersistentFlags().BoolVar(&config.AppOsAppendVersion, "os-append-version", config.AppOsAppendVersion, "")
			cmd.PersistentFlags().StringVar(&config.AppPairDisplayName, "pair-display-name", config.AppPairDisplayName, "")
			require.NoError(t, cmd.ParseFlags(tc.args))

			loadDeviceNameConfig(cmd)
			require.Equal(t, tc.wantAppendVersion, config.AppOsAppendVersion)
			require.Equal(t, tc.wantPairName, config.AppPairDisplayName)
		})
	}
}
