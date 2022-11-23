package cmd

import (
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/internal/rest"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/internal/rest/helpers"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/internal/websocket"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/middleware"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/services"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/dustin/go-humanize"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html"
	"github.com/markbates/pkger"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/cobra"
	"log"
	"os"
	"strings"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Short: "Send free whatsapp API",
	Long: `This application is from clone https://github.com/aldinokemal/go-whatsapp-web-multidevice, 
you can send whatsapp over http api but your whatsapp account have to be multi device version`,
	Run: runRest,
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.PersistentFlags().StringVarP(&config.AppPort, "port", "p", config.AppPort, "change port number with --port <number> | example: --port=8080")
	rootCmd.PersistentFlags().BoolVarP(&config.AppDebug, "debug", "d", config.AppDebug, "hide or displaying log with --debug <true/false> | example: --debug=true")
	rootCmd.PersistentFlags().StringVarP(&config.AppOs, "os", "", config.AppOs, `os name --os <string> | example: --os="Chrome"`)
	rootCmd.PersistentFlags().StringVarP(&config.AppBasicAuthCredential, "basic-auth", "b", config.AppBasicAuthCredential, "basic auth credential | yourUsername:yourPassword")
	rootCmd.PersistentFlags().StringVarP(&config.WhatsappAutoReplyMessage, "autoreply", "", config.WhatsappAutoReplyMessage, `auto reply when received message --autoreply <string> | example: --autoreply="Don't reply this message"`)
	rootCmd.PersistentFlags().StringVarP(&config.WhatsappAutoReplyWebhook, "webhook", "w", config.WhatsappAutoReplyMessage, `auto reply when received message --webhook <string> | example: --webhook="https://yourcallback.com/callback"`)
}

func runRest(_ *cobra.Command, _ []string) {
	if config.AppDebug {
		config.WhatsappLogLevel = "DEBUG"
	}

	// TODO: Init Rest App
	//preparing folder if not exist
	err := utils.CreateFolder(config.PathQrCode, config.PathSendItems, config.PathStorages)
	if err != nil {
		log.Fatalln(err)
	}

	engine := html.NewFileSystem(pkger.Dir("/views"), ".html")
	engine.AddFunc("isEnableBasicAuth", func(token any) bool {
		return token != nil
	})
	app := fiber.New(fiber.Config{
		Views:     engine,
		BodyLimit: 50 * 1024 * 1024,
	})
	app.Static("/statics", "./statics")
	app.Use(middleware.Recovery())
	app.Use(middleware.BasicAuth())
	if config.AppDebug {
		app.Use(logger.New())
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept",
	}))

	if config.AppBasicAuthCredential != "" {
		account := make(map[string]string, 0)
		multipleBA := strings.Split(config.AppBasicAuthCredential, ",")
		for _, basicAuth := range multipleBA {
			ba := strings.Split(basicAuth, ":")
			if len(ba) != 2 {
				log.Fatalln("Basic auth is not valid, please this following format <user>:<secret>")
			}
			account[ba[0]] = ba[1]
		}

		if account != nil {
			app.Use(basicauth.New(basicauth.Config{
				Users: account,
			}))
		}
	}

	db := utils.InitWaDB()
	cli := utils.InitWaCLI(db)

	// Service
	appService := services.NewAppService(cli, db)
	sendService := services.NewSendService(cli)
	userService := services.NewUserService(cli)

	// Rest
	rest.InitRestApp(app, appService)
	rest.InitRestSend(app, sendService)
	rest.InitRestUser(app, userService)

	app.Get("/", func(c *fiber.Ctx) error {
		return c.Render("index", fiber.Map{
			"AppHost":        fmt.Sprintf("%s://%s", c.Protocol(), c.Hostname()),
			"AppVersion":     config.AppVersion,
			"BasicAuthToken": c.UserContext().Value("token"),
			"MaxFileSize":    humanize.Bytes(uint64(config.WhatsappSettingMaxFileSize)),
			"MaxVideoSize":   humanize.Bytes(uint64(config.WhatsappSettingMaxVideoSize)),
		})
	})

	websocket.RegisterRoutes(app, appService)
	go websocket.RunHub()

	// Set auto reconnect to whatsapp server after booting
	go helpers.SetAutoConnectAfterBooting(appService)
	if err = app.Listen(":" + config.AppPort); err != nil {
		log.Fatalln("Failed to start: ", err.Error())
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
