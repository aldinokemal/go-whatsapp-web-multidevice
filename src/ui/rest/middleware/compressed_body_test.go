package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRejectCompressedBody(t *testing.T) {
	app := fiber.New()
	app.Use("/hook", RejectCompressedBody())
	reached := 0
	handler := func(c fiber.Ctx) error {
		reached++
		return c.SendString("ok")
	}
	app.Post("/hook", handler)
	app.Post("/hook/:device_id", handler)

	tests := []struct {
		path      string
		encodings []string
		want      int
	}{
		{"/hook", nil, fiber.StatusOK},
		{"/hook", []string{"identity"}, fiber.StatusOK},
		{"/hook", []string{"gzip"}, fiber.StatusUnsupportedMediaType},
		{"/hook", []string{"BR"}, fiber.StatusUnsupportedMediaType},
		{"/hook", []string{"identity, zstd"}, fiber.StatusUnsupportedMediaType},
		{"/hook", []string{"identity", "gzip"}, fiber.StatusUnsupportedMediaType},
		{"/hook/dev1", []string{"deflate"}, fiber.StatusUnsupportedMediaType},
	}
	for _, tt := range tests {
		reached = 0
		req := httptest.NewRequest(fiber.MethodPost, tt.path, strings.NewReader(`{}`))
		for _, encoding := range tt.encodings {
			req.Header.Add(fiber.HeaderContentEncoding, encoding)
		}
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, tt.want, resp.StatusCode, "%s %q", tt.path, tt.encodings)
		assert.Equal(t, tt.want == fiber.StatusOK, reached == 1, "%s %q handler reached", tt.path, tt.encodings)
	}
}
