package rest

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicStaticPath(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{
			name:     "media file under statics",
			filePath: "statics/media/628123/2026-06-09/audio file.ogg",
			want:     "/statics/media/628123/2026-06-09/audio%20file.ogg",
		},
		{
			name:     "windows separators",
			filePath: "statics\\media\\628123\\2026-06-09\\voice.ogg",
			want:     "/statics/media/628123/2026-06-09/voice.ogg",
		},
		{
			name:     "outside statics",
			filePath: "storages/audio.ogg",
			want:     "",
		},
		{
			name:     "path traversal",
			filePath: "../../etc/passwd",
			want:     "",
		},
		{
			name:     "empty path",
			filePath: "",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := publicStaticPath(tt.filePath); got != tt.want {
				t.Fatalf("publicStaticPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublicStaticFileURLUsesRequestScheme(t *testing.T) {
	oldBasePath := config.AppBasePath
	config.AppBasePath = "/api"
	defer func() {
		config.AppBasePath = oldBasePath
	}()

	app := fiber.New()
	app.Get("/url", func(c fiber.Ctx) error {
		return c.SendString(publicStaticFileURL(c, "statics/media/photo.jpg"))
	})

	resp, err := app.Test(httptest.NewRequest("GET", "http://example.com/url", nil))
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "http://example.com/api/statics/media/photo.jpg", string(body))
}
