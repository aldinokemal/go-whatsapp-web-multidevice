package main

import (
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/controllers"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/middleware"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/services"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/template/html"
	"github.com/markbates/pkger"
	_ "github.com/mattn/go-sqlite3"
	"log"
)

func main() {
	// preparing folder if not exist
	err := utils.CreateFolder(config.PathQrCode, config.PathSendItems)
	if err != nil {
		log.Fatalln(err)
	}

	engine := html.NewFileSystem(pkger.Dir("/views"), ".html")
	app := fiber.New(fiber.Config{
		Views:     engine,
		BodyLimit: 10 * 1024 * 1024,
	})
	app.Static("/statics", "./statics")
	app.Use(middleware.Recovery())
	app.Use(logger.New())
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

	appController.Route(app)
	sendController.Route(app)
	userController.Route(app)

	app.Get("/", func(ctx *fiber.Ctx) error {
		return ctx.Render("index", fiber.Map{"AppHost": fmt.Sprintf("%s://%s", ctx.Protocol(), ctx.Hostname())})
	})

	err = app.Listen(":" + config.AppPort)
	if err != nil {
		log.Fatalln("Failed to start: ", err.Error())
	}
}
