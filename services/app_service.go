package services

import (
	"github.com/aldinokemal/go-whatsapp-web-multidevice/structs"
	"github.com/gofiber/fiber/v2"
)

type AppService interface {
	Login(c *fiber.Ctx) (response structs.LoginResponse, err error)
	Logout(c *fiber.Ctx) (err error)
	Reconnect(c *fiber.Ctx) (err error)
}
