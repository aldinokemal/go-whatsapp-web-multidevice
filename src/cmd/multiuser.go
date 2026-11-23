package cmd

import (
	"os"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/gofiber/fiber/v3"
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

// setupMultiUser is called by restServer when WTF_MULTIUSER=true. It must run
// before any apiGroup registration so the API gate precedes upstream routes.
func setupMultiUser(app *fiber.App, dm *whatsapp.DeviceManager, s multiuser.Settings) (*multiuser.Runtime, error) {
	// multiuser.Apply() already ran at the top of restServer.
	rt, err := multiuser.Setup(s, dm, deviceUsecase, config.PathStorages)
	if err != nil {
		return nil, err
	}
	deviceUsecase = rt.Devices // /devices REST routes now see only the caller's devices
	rt.Install(app, dm, mcpUsecaseDeps())
	// Finish any account deletion left half done, and free link slots nobody
	// scanned. The retention worker (phase 3) is wired separately.
	var chats multiuser.ChatData
	if chatStorageRepo != nil {
		chats = chatStorageRepo
	}
	rt.Start(chats)
	return rt, nil
}

// mcpUsecaseDeps are the usecase instances the MCP tools call: the same ones
// the REST handlers hold, so both surfaces share one whatsmeow session.
func mcpUsecaseDeps() uimcp.Deps {
	return uimcp.Deps{
		App:      appUsecase,
		Send:     sendUsecase,
		Schedule: scheduleUsecase,
		Chat:     chatUsecase,
		User:     userUsecase,
		Message:  messageUsecase,
		Group:    groupUsecase,
	}
}
