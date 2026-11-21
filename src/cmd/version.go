package cmd

import (
	"fmt"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/spf13/cobra"
)

// versionCmd prints the bare version (e.g. "9.6.0") so scripts can consume it.
// The app initializers in root.go are skipped for this command, keeping stdout
// free of config output and leaving no storage folders or databases behind.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the application version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), strings.TrimPrefix(config.AppVersion, "v"))
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
