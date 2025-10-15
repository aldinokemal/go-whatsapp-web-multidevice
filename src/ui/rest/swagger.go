package rest

import (
	_ "embed"
	"fmt"
	
	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/swagger"
)

//go:embed openapi.yaml
var openapiSpec []byte

func InitSwagger(app fiber.Router) {
	specURL := fmt.Sprintf("%s/swagger/openapi.yaml", config.AppBasePath)
	if config.AppBasePath == "" {
		specURL = "/swagger/openapi.yaml"
	}
	
	// Register the OpenAPI spec endpoint first (before the wildcard route)
	app.Get("/swagger/openapi.yaml", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "application/x-yaml")
		return c.Send(openapiSpec)
	})

	cfg := swagger.Config{
		Title:        "WhatsApp API MultiDevice",
		URL:          specURL,
		DeepLinking:  true,
		DocExpansion: "none",
	}

	// Register the swagger UI handler with wildcard
	app.Get("/swagger/*", swagger.New(cfg))
}