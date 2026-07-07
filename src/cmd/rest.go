package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/helpers"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/dustin/go-humanize"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html/v2"
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
	engine := html.NewFileSystem(http.FS(EmbedIndex), ".html")
	engine.AddFunc("isEnableBasicAuth", func(token any) bool {
		return token != nil
	})
	fiberConfig := fiber.Config{
		Views:                   engine,
		EnableTrustedProxyCheck: true,
		BodyLimit:               int(config.WhatsappSettingMaxVideoSize),
		Network:                 "tcp",
	}

	// Configure proxy settings if trusted proxies are specified
	if len(config.AppTrustedProxies) > 0 {
		fiberConfig.TrustedProxies = config.AppTrustedProxies
		fiberConfig.ProxyHeader = fiber.HeaderXForwardedHost
	}

	app := fiber.New(fiberConfig)

	app.Static(config.AppBasePath+"/statics", "./statics")
	app.Use(config.AppBasePath+"/components", filesystem.New(filesystem.Config{
		Root:       http.FS(EmbedViews),
		PathPrefix: "views/components",
		Browse:     true,
	}))
	app.Use(config.AppBasePath+"/assets", filesystem.New(filesystem.Config{
		Root:       http.FS(EmbedViews),
		PathPrefix: "views/assets",
		Browse:     true,
	}))

	app.Use(middleware.Recovery())
	app.Use(middleware.RequestTimeout(middleware.DefaultRequestTimeout))
	app.Use(middleware.BasicAuth())
	if config.AppDebug {
		app.Use(logger.New())
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept",
	}))

	// Device manager - needed for chatwoot webhook and health check
	dm := whatsapp.GetDeviceManager()

	// Health check endpoint (public, no auth)
	// Registered at root path (ignoring AppBasePath) to ensure fixed availability
	// for infrastructure health probes (Kubernetes liveness/readiness, Docker healthcheck, etc.)
	app.Get("/health", func(c *fiber.Ctx) error {
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
		// Auto-provision the Chatwoot inbox (create or reuse) when enabled, so
		// CHATWOOT_INBOX_ID is resolved before any message is forwarded. Failures
		// are logged but non-fatal — the operator can still set the inbox manually.
		if config.ChatwootAutoCreate {
			if err := chatwoot.EnsureInbox(chatwoot.GetDefaultClient()); err != nil {
				logrus.Errorf("Chatwoot auto-create failed: %v", err)
			}
		}
		whatsapp.StartChatwootForwardRetryWorker(chatStorageRepo)

		chatwootHandler = rest.NewChatwootHandler(appUsecase, sendUsecase, messageUsecase, dm, chatStorageRepo)
		webhookPath := "/chatwoot/webhook"
		if config.AppBasePath != "" {
			webhookPath = config.AppBasePath + webhookPath
		}
		app.Post(webhookPath, chatwootHandler.HandleWebhook)
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

		app.Use(basicauth.New(basicauth.Config{
			Users: account,
		}))
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

	// Device-scoped operations (header-based)
	headerDeviceGroup := apiGroup.Group("", middleware.DeviceMiddleware(dm))
	registerDeviceScopedRoutes(headerDeviceGroup)

	// Chatwoot sync routes - require authentication (webhook is registered earlier without auth)
	if config.ChatwootEnabled {
		apiGroup.Post("/chatwoot/sync", chatwootHandler.SyncHistory)
		apiGroup.Get("/chatwoot/sync/status", chatwootHandler.SyncStatus)
	}

	apiGroup.Get("/", func(c *fiber.Ctx) error {
		return c.Render("views/index", fiber.Map{
			"AppHost":        fmt.Sprintf("%s://%s", c.Protocol(), c.Hostname()),
			"AppVersion":     config.AppVersion,
			"AppBasePath":    config.AppBasePath,
			"BasicAuthToken": c.UserContext().Value(middleware.AuthorizationValue("BASIC_AUTH")),
			"MaxFileSize":    humanize.Bytes(uint64(config.WhatsappSettingMaxFileSize)),
			"MaxVideoSize":   humanize.Bytes(uint64(config.WhatsappSettingMaxVideoSize)),
		})
	})

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
		listenErr <- app.Listen(config.AppHost + ":" + config.AppPort)
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
		// Release the Chatwoot direct-Postgres importer pool if one was
		// opened. Safe when Chatwoot is disabled or the pool was never
		// initialized — GetDefaultSyncService() returns nil.
		if svc := chatwoot.GetDefaultSyncService(); svc != nil {
			if err := svc.Close(); err != nil {
				logrus.Warnf("Chatwoot sync close: %v", err)
			}
		}
		if chatStorageDB != nil {
			if err := chatStorageDB.Close(); err != nil {
				logrus.Warnf("Chat storage close: %v", err)
			}
		}
	}
}
