package cmd

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMcpOAuthMiddlewareOnlyProtectsMcpRoute(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
	}{
		{name: "root deployment"},
		{name: "base path deployment", basePath: "/gowa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			var router fiber.Router = app
			if tt.basePath != "" {
				router = app.Group(tt.basePath)
			}

			useMcpOAuthMiddleware(router, func(c fiber.Ctx) error {
				c.Set(fiber.HeaderWWWAuthenticate, `Bearer resource_metadata="https://gowa.example.com/.well-known/oauth-protected-resource/mcp"`)
				return c.SendStatus(fiber.StatusUnauthorized)
			})
			router.Post("/mcp", func(c fiber.Ctx) error {
				return c.SendStatus(fiber.StatusNoContent)
			})

			app.Use(newBasicAuthMiddleware(map[string]string{"user": "secret"}))
			app.Get(tt.basePath+"/", func(c fiber.Ctx) error {
				return c.SendStatus(fiber.StatusOK)
			})
			app.Get("/favicon.ico", func(c fiber.Ctx) error {
				return c.SendStatus(fiber.StatusOK)
			})

			mcpResp, err := app.Test(httptest.NewRequest("POST", tt.basePath+"/mcp", nil))
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusUnauthorized, mcpResp.StatusCode)
			assert.Contains(t, mcpResp.Header.Get(fiber.HeaderWWWAuthenticate), "Bearer")

			rootResp, err := app.Test(httptest.NewRequest("GET", tt.basePath+"/", nil))
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusUnauthorized, rootResp.StatusCode)
			assert.Equal(t, `Basic realm="Restricted", charset="UTF-8"`, rootResp.Header.Get(fiber.HeaderWWWAuthenticate))

			faviconResp, err := app.Test(httptest.NewRequest("GET", "/favicon.ico", nil))
			require.NoError(t, err)
			assert.Equal(t, fiber.StatusUnauthorized, faviconResp.StatusCode)
			assert.Equal(t, `Basic realm="Restricted", charset="UTF-8"`, faviconResp.Header.Get(fiber.HeaderWWWAuthenticate))
		})
	}
}
