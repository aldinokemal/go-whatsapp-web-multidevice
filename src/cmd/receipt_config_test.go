package cmd

import (
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/stretchr/testify/require"
)

func TestReceiptConfigPrecedence(t *testing.T) {
	// Each process isolates Viper and the other globals changed by initEnvConfig.
	if os.Getenv("GOWA_RECEIPT_CONFIG_HELPER") == "1" {
		original := config.WhatsappWebhookAllDeviceReceipts
		flag := rootCmd.PersistentFlags().Lookup("webhook-all-device-receipts")
		originalChanged := flag.Changed
		defer func() {
			config.WhatsappWebhookAllDeviceReceipts = original
			flag.Changed = originalChanged
		}()
		if value := os.Getenv("GOWA_RECEIPT_TEST_FLAG"); value != "" {
			require.NoError(t, rootCmd.ParseFlags([]string{"--webhook-all-device-receipts=" + value}))
		}
		initEnvConfig()
		want, err := strconv.ParseBool(os.Getenv("GOWA_RECEIPT_TEST_WANT"))
		require.NoError(t, err)
		require.Equal(t, want, config.WhatsappWebhookAllDeviceReceipts)
		return
	}

	for _, tc := range []struct {
		name, env, flag string
		want            bool
	}{
		{"default disabled", "", "", false},
		{"environment enabled", "true", "", true},
		{"environment disabled", "false", "", false},
		{"explicit false overrides environment true", "true", "false", false},
		{"explicit true overrides environment false", "false", "true", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestReceiptConfigPrecedence$")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(),
				"GOWA_RECEIPT_CONFIG_HELPER=1",
				"WHATSAPP_WEBHOOK_ALL_DEVICE_RECEIPTS="+tc.env,
				"GOWA_RECEIPT_TEST_FLAG="+tc.flag,
				"GOWA_RECEIPT_TEST_WANT="+strconv.FormatBool(tc.want))
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}
