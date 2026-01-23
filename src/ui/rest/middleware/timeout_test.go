package middleware

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func TestRequestTimeout_SetsDeadline(t *testing.T) {
	app := fiber.New()
	app.Use(RequestTimeout(5 * time.Second))

	var capturedCtx context.Context
	app.Get("/test", func(c *fiber.Ctx) error {
		capturedCtx = c.UserContext()
		return c.SendString("ok")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req, -1)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	// Verify context has a deadline
	deadline, hasDeadline := capturedCtx.Deadline()
	assert.True(t, hasDeadline, "context should have a deadline")
	assert.WithinDuration(t, time.Now().Add(5*time.Second), deadline, 1*time.Second)
}

func TestRequestTimeout_CancelsOnTimeout(t *testing.T) {
	app := fiber.New()
	app.Use(RequestTimeout(50 * time.Millisecond))

	app.Get("/slow", func(c *fiber.Ctx) error {
		ctx := c.UserContext()
		select {
		case <-time.After(200 * time.Millisecond):
			return c.SendString("completed")
		case <-ctx.Done():
			return c.Status(504).SendString("timeout")
		}
	})

	req := httptest.NewRequest("GET", "/slow", nil)
	resp, err := app.Test(req, 500)
	assert.NoError(t, err)
	assert.Equal(t, 504, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "timeout", string(body))
}

func TestDefaultRequestTimeout_Value(t *testing.T) {
	assert.Equal(t, 45*time.Second, DefaultRequestTimeout)
}
