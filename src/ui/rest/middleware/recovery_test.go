package middleware

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
)

func TestRecoveryMapsWrappedDeadlineToGatewayTimeout(t *testing.T) {
	app := fiber.New()
	app.Use(Recovery())
	app.Get("/slow", func(c fiber.Ctx) error {
		panic(fmt.Errorf("failed to check 628123 on WhatsApp: %w", context.DeadlineExceeded))
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/slow", nil))
	assert.NoError(t, err)
	assert.Equal(t, 504, resp.StatusCode)
}
