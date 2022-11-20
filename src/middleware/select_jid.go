package middleware

import (
	"context"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v2"
)

func SelectJid() fiber.Handler {
	return func(c *fiber.Ctx) error {
		selectedJid := string(c.Request().Header.Peek("jid"))
		if selectedJid == "" {
			selectedJid = config.AppDefaultDevice
		}

		ctx := context.WithValue(c.Context(), config.AppSelectedDeviceKey, selectedJid)
		c.SetUserContext(ctx)

		return c.Next()
	}
}
