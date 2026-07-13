package cmd

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicAuthMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		username   string
		password   string
		wantStatus int
	}{
		{
			name:       "accepts configured plaintext credential",
			username:   "user",
			password:   "secret",
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "rejects wrong password",
			username:   "user",
			password:   "wrong",
			wantStatus: fiber.StatusUnauthorized,
		},
		{
			name:       "rejects unknown user",
			username:   "unknown",
			password:   "secret",
			wantStatus: fiber.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(newBasicAuthMiddleware(map[string]string{"user": "secret"}))
			app.Get("/", func(c fiber.Ctx) error {
				return c.SendStatus(fiber.StatusOK)
			})

			req := httptest.NewRequest("GET", "/", nil)
			req.SetBasicAuth(tt.username, tt.password)

			resp, err := app.Test(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}
}
