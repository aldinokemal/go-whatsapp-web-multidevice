package cmd

import (
	"os"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/spf13/viper"
)

// initAppFunc is initApp; a var so tests can observe whether it runs.
var initAppFunc = initApp

// initAppUnlessSkipped runs initApp unless the command opted out (admin-link).
func initAppUnlessSkipped(args []string) {
	if commandSkipsAppInit(args) {
		return
	}
	initAppFunc()
}

// settingGetenv returns the process environment value for k, else the value
// utils.LoadConfig put into viper from a local .env (which does not populate
// os.Environ). viper keys are case-insensitive.
func settingGetenv(k string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return viper.GetString(k)
}

// loadMultiUserSettings reads WTF_* settings from the environment (Render
// injects them) falling back to the local .env as loaded into viper.
func loadMultiUserSettings() (multiuser.Settings, error) {
	return multiuser.Load(settingGetenv)
}
