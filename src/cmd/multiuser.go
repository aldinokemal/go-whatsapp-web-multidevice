package cmd

import (
	"os"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
)

// loadMultiUserSettings reads WTF_* settings straight from the environment
// (Render injects them; .env is already loaded into the process by utils.LoadConfig).
func loadMultiUserSettings() (multiuser.Settings, error) {
	return multiuser.Load(os.Getenv)
}
