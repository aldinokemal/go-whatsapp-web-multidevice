package middleware

import (
	"context"
	"github.com/gofiber/fiber/v2"
)

func BasicAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := string(c.Request().Header.Peek("Authorization"))
		if token != "" {
			ctx := context.WithValue(c.Context(), "token", token)
			c.SetUserContext(ctx)
		}

		return c.Next()
	}
}
