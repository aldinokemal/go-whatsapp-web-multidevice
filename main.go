package main

import (
	"fmt"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/controllers"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/services"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/html"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	engine := html.New("./views", ".html")
	app := fiber.New(fiber.Config{
		Views: engine,
	})
	app.Static("/statics", "./statics")
	app.Use(recover.New())
	app.Use(logger.New())

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
		return ctx.JSON(map[string]interface{}{"Status": "Ok"})
	})

	err := app.Listen(":3000")
	if err != nil {
		fmt.Println("Failed to start: ", err.Error())
	}
}
