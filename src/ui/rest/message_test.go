package rest

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type playedMessageServiceStub struct {
	domainMessage.IMessageUsecase
	request *domainMessage.MarkAsPlayedRequest
}

func (stub *playedMessageServiceStub) MarkAsPlayed(_ context.Context, request domainMessage.MarkAsPlayedRequest) (domainMessage.GenericResponse, error) {
	stub.request = &request
	return domainMessage.GenericResponse{MessageID: request.MessageID, Status: "played"}, nil
}

func TestMarkAsPlayedRouteDelegatesToMessageService(t *testing.T) {
	app := fiber.New()
	service := &playedMessageServiceStub{}
	InitRestMessage(app, service, nil)

	request := httptest.NewRequest(
		"POST",
		"/message/voice-message-1/played",
		strings.NewReader(`{"phone":"628123456789"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, response.StatusCode)
	require.NotNil(t, service.request)
	require.Equal(t, "voice-message-1", service.request.MessageID)
	require.Equal(t, "628123456789@s.whatsapp.net", service.request.Phone)
}

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

// TestPublicStaticFileURLKeepsRequestPort guards against dropping the port the
// client actually connected to, which makes the returned URL unreachable when
// the app is served on a non-default port.
func TestPublicStaticFileURLKeepsRequestPort(t *testing.T) {
	app := fiber.New()
	app.Get("/url", func(c fiber.Ctx) error {
		return c.SendString(publicStaticFileURL(c, "statics/media/photo.jpg"))
	})

	resp, err := app.Test(httptest.NewRequest("GET", "http://172.168.0.101:3000/url", nil))
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "http://172.168.0.101:3000/statics/media/photo.jpg", string(body))
}
