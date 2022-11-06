package cmd

import (
	"encoding/base64"
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/controllers"
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
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
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
	rootCmd.PersistentFlags().StringVarP(&config.AppBasicAuthCredential, "basic-auth", "b", config.AppBasicAuthCredential, "basic auth credential | yourUsername:yourPassword")
	rootCmd.PersistentFlags().StringVarP(&config.WhatsappAutoReplyMessage, "autoreply", "", config.WhatsappAutoReplyMessage, `auto reply when received message --autoreply <string> | example: --autoreply="Don't reply this message"`)
}

func runRest(cmd *cobra.Command, args []string) {
	if config.AppDebug {
		config.WhatsappLogLevel = "DEBUG"
	}

	// TODO: Init Rest App
	//preparing folder if not exist
	err := utils.CreateFolder(config.PathQrCode, config.PathSendItems)
	if err != nil {
		log.Fatalln(err)
	}

	engine := html.NewFileSystem(pkger.Dir("/views"), ".html")
	engine.AddFunc("isEnableBasicAuth", func(token string) bool {
		return token != ""
	})
	app := fiber.New(fiber.Config{
		Views:     engine,
		BodyLimit: 50 * 1024 * 1024,
	})
	app.Static("/statics", "./statics")
	app.Use(middleware.Recovery())
	if config.AppDebug {
		app.Use(logger.New())
	}
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept",
	}))

	db := utils.InitWaDB()
	cli := utils.InitWaCLI(db)

	// Service
	appService := services.NewAppService(cli)
	sendService := services.NewSendService(cli)
	userService := services.NewUserService(cli)

	// Controller
	appController := controllers.NewAppController(appService)
	sendController := controllers.NewSendController(sendService)
	userController := controllers.NewUserController(userService)

	if config.AppBasicAuthCredential != "" {
		ba := strings.Split(config.AppBasicAuthCredential, ":")
		if len(ba) != 2 {
			log.Fatalln("Basic auth is not valid, please this following format <user>:<secret>")
		}

		app.Use(basicauth.New(basicauth.Config{
			Users: map[string]string{
				ba[0]: ba[1],
			},
		}))
	}

	appController.Route(app)
	sendController.Route(app)
	userController.Route(app)

	app.Get("/", func(ctx *fiber.Ctx) error {
		return ctx.Render("index", fiber.Map{
			"AppHost":        fmt.Sprintf("%s://%s", ctx.Protocol(), ctx.Hostname()),
			"AppVersion":     config.AppVersion,
			"BasicAuthToken": base64.StdEncoding.EncodeToString([]byte(config.AppBasicAuthCredential)),
			"MaxFileSize":    humanize.Bytes(uint64(config.WhatsappSettingMaxFileSize)),
			"MaxVideoSize":   humanize.Bytes(uint64(config.WhatsappSettingMaxVideoSize)),
		})
	})

	err = app.Listen(":" + config.AppPort)
	if err != nil {
		log.Fatalln("Failed to start: ", err.Error())
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
