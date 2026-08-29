package rest

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type newsletterServiceStub struct {
	domainNewsletter.INewsletterUsecase
	request       *domainNewsletter.DownloadMediaRequest
	contextDevice *whatsapp.DeviceInstance
}

func (stub *newsletterServiceStub) DownloadMedia(ctx context.Context, request domainNewsletter.DownloadMediaRequest) (domainNewsletter.DownloadMediaResponse, error) {
	stub.request = &request
	stub.contextDevice, _ = whatsapp.DeviceFromContext(ctx)
	return domainNewsletter.DownloadMediaResponse{
		ServerID:  request.ServerID,
		MessageID: "ACBD7F85529AAD63FAF376F622351D14",
		Status:    "Media downloaded successfully",
		MediaType: "image",
		MimeType:  "image/jpeg",
		Filename:  "result.jpg",
		FilePath:  "statics/media/120363123456789/2026-08-28/result.jpg",
		FileSize:  1234,
	}, nil
}

func TestDownloadNewsletterMediaRoute(t *testing.T) {
	oldBasePath := config.AppBasePath
	config.AppBasePath = "/api"
	defer func() {
		config.AppBasePath = oldBasePath
	}()

	device := whatsapp.NewDeviceInstance("device-a", nil, nil)
	service := &newsletterServiceStub{}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("device", device)
		return c.Next()
	})
	InitRestNewsletter(app, service)

	request := httptest.NewRequest(
		"GET",
		"http://example.com/newsletter/messages/8872/download?newsletter_id=120363123456789%40newsletter",
		nil,
	)
	response, err := app.Test(request)

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, response.StatusCode)
	require.NotNil(t, service.request)
	assert.Equal(t, 8872, service.request.ServerID)
	assert.Equal(t, "120363123456789@newsletter", service.request.NewsletterID)
	assert.Same(t, device, service.contextDevice)

	var body struct {
		Results domainNewsletter.DownloadMediaResponse `json:"results"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&body))
	assert.Equal(t, "http://example.com/api/statics/media/120363123456789/2026-08-28/result.jpg", body.Results.FileURL)
}
