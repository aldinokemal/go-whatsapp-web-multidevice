package cmd

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/uiasset"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/helpers"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/basicauth"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var restCmd = &cobra.Command{
	Use:   "rest",
	Short: "Send whatsapp API over http",
	Long:  `This application is from clone https://github.com/aldinokemal/go-whatsapp-web-multidevice`,
	Run:   restServer,
}

func init() {
	rootCmd.AddCommand(restCmd)
}
func restServer(_ *cobra.Command, _ []string) {
	fiberConfig := fiber.Config{
		TrustProxy: true,
		BodyLimit:  int(config.WhatsappSettingMaxVideoSize),
	}

	// Configure proxy settings if trusted proxies are specified
	if len(config.AppTrustedProxies) > 0 {
		fiberConfig.TrustProxyConfig = fiber.TrustProxyConfig{Proxies: config.AppTrustedProxies}
		fiberConfig.ProxyHeader = fiber.HeaderXForwardedHost
	}

	app := fiber.New(fiberConfig)

	// CORS must precede the static routes so cross-origin UIs (gowa-ui) can
	// fetch media under /statics and authenticate with the headers below.
	app.Use(newCORSMiddleware())

	app.Use(config.AppBasePath+"/statics", static.New("./statics"))

	app.Use(middleware.Recovery())
	app.Use(middleware.RequestTimeout(middleware.DefaultRequestTimeout))
	if config.AppDebug {
		app.Use(logger.New())
	}

	// Device manager - needed for chatwoot webhook and health check
	dm := whatsapp.GetDeviceManager()

	// Health check endpoint (public, no auth)
	// Registered at root path (ignoring AppBasePath) to ensure fixed availability
	// for infrastructure health probes (Kubernetes liveness/readiness, Docker healthcheck, etc.)
	app.Get("/health", func(c fiber.Ctx) error {
		if dm != nil && dm.IsHealthy() {
			return c.SendString("OK")
		}
		return c.Status(http.StatusServiceUnavailable).SendString("Service Unavailable")
	})

	// Chatwoot webhook - registered BEFORE basic auth middleware
	// This allows Chatwoot to send webhooks without authentication. The handler
	// is stateless and shared with the authenticated sync routes registered below.
	var chatwootHandler *rest.ChatwootHandler
	if config.ChatwootEnabled {
		// Auto-provision the inbox, install the per-device client registry, then
		// start the retry worker (registry before worker — see initChatwootForwarding).
		initChatwootForwarding(chatStorageRepo)

		chatwootHandler = rest.NewChatwootHandler(appUsecase, sendUsecase, messageUsecase, dm, chatStorageRepo)
		webhookPath := "/chatwoot/webhook"
		if config.AppBasePath != "" {
			webhookPath = config.AppBasePath + webhookPath
		}
		app.Post(webhookPath, chatwootHandler.HandleWebhook)
		// Per-device webhook: each device's Chatwoot inbox is configured to POST
		// here so agent replies route deterministically to the right device.
		app.Post(webhookPath+"/:device_id", chatwootHandler.HandleDeviceWebhook)
	}

	if len(config.AppBasicAuthCredential) > 0 {
		account := make(map[string]string)
		for _, basicAuth := range config.AppBasicAuthCredential {
			ba := strings.Split(basicAuth, ":")
			if len(ba) != 2 {
				logrus.Fatalln("Basic auth is not valid, please this following format <user>:<secret>")
			}
			account[ba[0]] = ba[1]
		}

		app.Use(middleware.WebsocketQueryAuth())
		app.Use(newBasicAuthMiddleware(account))
	}

	// Create base path group or use app directly
	var apiGroup fiber.Router = app
	if config.AppBasePath != "" {
		apiGroup = app.Group(config.AppBasePath)
	}

	registerDeviceScopedRoutes := func(r fiber.Router) {
		rest.InitRestApp(r, appUsecase)
		rest.InitRestCall(r, callUsecase)
		rest.InitRestChat(r, chatUsecase)
		rest.InitRestSend(r, sendUsecase)
		rest.InitRestUser(r, userUsecase)
		rest.InitRestMessage(r, messageUsecase, sendUsecase)
		rest.InitRestGroup(r, groupUsecase)
		rest.InitRestNewsletter(r, newsletterUsecase)
		websocket.RegisterRoutes(r, appUsecase)
	}

	// Device management routes (no device_id required)
	rest.InitRestDevice(apiGroup, deviceUsecase)

	// App info (version, limits) for standalone UIs; no device required
	rest.InitRestAppInfo(apiGroup)

	// Device-scoped operations (header-based)
	headerDeviceGroup := apiGroup.Group("", middleware.DeviceMiddleware(dm))
	registerDeviceScopedRoutes(headerDeviceGroup)

	// Chatwoot sync + per-device config routes - require authentication (the
	// webhooks are registered earlier without auth).
	if config.ChatwootEnabled {
		apiGroup.Post("/chatwoot/sync", chatwootHandler.SyncHistory)
		apiGroup.Get("/chatwoot/sync/status", chatwootHandler.SyncStatus)
		apiGroup.Get("/chatwoot/configs", chatwootHandler.ListChatwootConfigs)
		apiGroup.Get("/devices/:device_id/chatwoot/config", chatwootHandler.GetChatwootConfig)
		apiGroup.Put("/devices/:device_id/chatwoot/config", chatwootHandler.UpsertChatwootConfig)
		apiGroup.Delete("/devices/:device_id/chatwoot/config", chatwootHandler.DeleteChatwootConfig)
	}

	// Dashboard: gowa-ui is a separate project released as one HTML file;
	// serve the runtime-downloaded copy at "/" (behind basic auth like the
	// rest of the API surface).
	uiCtx, uiCancel := context.WithCancel(context.Background())
	defer uiCancel()
	registerUIRoute(apiGroup, uiCtx)

	go websocket.RunHub()

	// Set auto reconnect to whatsapp server after booting
	go helpers.SetAutoConnectAfterBooting(appUsecase)

	// Set auto reconnect checking with a guaranteed client instance
	startAutoReconnectCheckerIfClientAvailable()

	// Set daily presence pulse scheduler when enabled
	startPresencePulseSchedulerIfEnabled()

	// Listen in a goroutine so we can trap SIGINT/SIGTERM and drain the
	// server cleanly. Without this, Fiber's Listen blocks until the OS
	// kills the process, leaking the Chatwoot Postgres importer pool and
	// the chat storage DB connection.
	listenErr := make(chan error, 1)
	go func() {
		listenErr <- app.Listen(config.AppHost+":"+config.AppPort, fiber.ListenConfig{ListenerNetwork: "tcp"})
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-listenErr:
		if err != nil {
			logrus.Fatalln("Failed to start: ", err.Error())
		}
	case sig := <-sigCh:
		logrus.Infof("Received %s — shutting down", sig)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			logrus.Warnf("HTTP server shutdown: %v", err)
		}
		// Release any Chatwoot direct-Postgres importer pools opened by per-device
		// sync services. Safe when Chatwoot is disabled or none were initialized.
		if err := chatwoot.CloseAllSyncServices(); err != nil {
			logrus.Warnf("Chatwoot sync close: %v", err)
		}
		if chatStorageDB != nil {
			if err := chatStorageDB.Close(); err != nil {
				logrus.Warnf("Chat storage close: %v", err)
			}
		}
	}
}

func registerUIRoute(apiGroup fiber.Router, ctx context.Context) {
	if !config.AppUIEnabled {
		apiGroup.Get("/", func(c fiber.Ctx) error {
			return c.JSON(utils.ResponseData{
				Status:  200,
				Code:    "SUCCESS",
				Message: fmt.Sprintf("gowa %s — UI disabled", config.AppVersion),
			})
		})
		return
	}

	uiManager := uiasset.New(uiasset.Config{
		Repo:         config.AppUIRepo,
		AssetName:    config.AppUIAssetName,
		CacheDir:     config.PathUICache,
		GithubToken:  config.AppUIGithubToken,
		Interval:     config.AppUIUpdateInterval,
		PinnedSHA256: config.AppUIAssetSHA256,
	})
	if err := uiManager.LoadCache(); err != nil {
		logrus.Infof("[UI_ASSET] no cached dashboard yet: %v", err)
	}
	if config.AppUIAutoUpdate {
		go func() {
			if err := uiManager.EnsureLatest(ctx); err != nil {
				logrus.Warnf("[UI_ASSET] initial dashboard download failed: %v", err)
			}
		}()
		go uiManager.StartAutoUpdate(ctx)
	}

	apiGroup.Get("/", func(c fiber.Ctx) error {
		content, etag, ok := uiManager.Content()
		if !ok {
			c.Type("html")
			return c.Send(uiasset.FallbackHTML(config.AppVersion, config.AppUIRepo))
		}
		quoted := `"` + etag + `"`
		c.Set(fiber.HeaderCacheControl, "no-cache")
		c.Set(fiber.HeaderETag, quoted)
		if c.Get(fiber.HeaderIfNoneMatch) == quoted {
			return c.SendStatus(fiber.StatusNotModified)
		}
		c.Type("html")
		return c.Send(content)
	})
}

func newCORSMiddleware() fiber.Handler {
	origins := []string{"*"}
	if len(config.AppCORSAllowedOrigins) > 0 {
		origins = config.AppCORSAllowedOrigins
	}
	return cors.New(cors.Config{
		AllowOrigins: origins,
		AllowMethods: []string{"GET", "POST", "HEAD", "PUT", "PATCH", "DELETE"},
		AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization", middleware.DeviceIDHeader},
	})
}

func newBasicAuthMiddleware(accounts map[string]string) fiber.Handler {
	return basicauth.New(basicauth.Config{
		Authorizer: func(username, password string, _ fiber.Ctx) bool {
			expectedPassword, ok := accounts[username]
			if !ok {
				return false
			}

			return subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) == 1
		},
	})
}
