package rest

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainNewsletter "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/newsletter"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/types"
)

type newsletterServiceStub struct {
	domainNewsletter.INewsletterUsecase
	request          *domainNewsletter.DownloadMediaRequest
	infoRequest      *domainNewsletter.GetInfoRequest
	directoryRequest *domainNewsletter.DirectoryRequest
	updatesRequest   *domainNewsletter.GetMessageUpdatesRequest
	liveRequest      *domainNewsletter.LiveUpdatesRequest
	followRequest    *domainNewsletter.FollowRequest
	contextDevice    *whatsapp.DeviceInstance
}

func (stub *newsletterServiceStub) Follow(ctx context.Context, request domainNewsletter.FollowRequest) error {
	stub.followRequest = &request
	stub.contextDevice, _ = whatsapp.DeviceFromContext(ctx)
	return nil
}

func (stub *newsletterServiceStub) GetInfo(ctx context.Context, request domainNewsletter.GetInfoRequest) (types.NewsletterMetadata, error) {
	stub.infoRequest = &request
	return types.NewsletterMetadata{ID: types.NewJID("120363123456789", types.NewsletterServer)}, nil
}

func (stub *newsletterServiceStub) Directory(ctx context.Context, request domainNewsletter.DirectoryRequest) (domainNewsletter.DirectoryResponse, error) {
	stub.directoryRequest = &request
	return domainNewsletter.DirectoryResponse{Data: []types.NewsletterMetadata{}}, nil
}

func (stub *newsletterServiceStub) GetMessageUpdates(ctx context.Context, request domainNewsletter.GetMessageUpdatesRequest) (domainNewsletter.GetMessagesResponse, error) {
	stub.updatesRequest = &request
	return domainNewsletter.GetMessagesResponse{}, nil
}

func (stub *newsletterServiceStub) SubscribeLiveUpdates(ctx context.Context, request domainNewsletter.LiveUpdatesRequest) (domainNewsletter.LiveUpdatesSubscription, error) {
	stub.liveRequest = &request
	return domainNewsletter.LiveUpdatesSubscription{NewsletterID: request.NewsletterID, DurationSeconds: 300}, nil
}

func (stub *newsletterServiceStub) UnsubscribeLiveUpdates(ctx context.Context, request domainNewsletter.LiveUpdatesRequest) error {
	stub.liveRequest = &request
	return nil
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

func newNewsletterTestApp(service *newsletterServiceStub, device *whatsapp.DeviceInstance) *fiber.App {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("device", device)
		return c.Next()
	})
	InitRestNewsletter(app, service)
	return app
}

func TestNewsletterFollowRoute(t *testing.T) {
	device := whatsapp.NewDeviceInstance("device-a", nil, nil)
	service := &newsletterServiceStub{}
	app := newNewsletterTestApp(service, device)

	request := httptest.NewRequest("POST", "/newsletter/follow", strings.NewReader(`{"newsletter_id":"120363123456789@newsletter"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, response.StatusCode)
	require.NotNil(t, service.followRequest)
	assert.Equal(t, "120363123456789@newsletter", service.followRequest.NewsletterID)
	assert.Same(t, device, service.contextDevice)
}

func TestNewsletterInfoRouteByInvite(t *testing.T) {
	service := &newsletterServiceStub{}
	app := newNewsletterTestApp(service, whatsapp.NewDeviceInstance("device-a", nil, nil))

	response, err := app.Test(httptest.NewRequest("GET", "/newsletter/info?invite=https%3A%2F%2Fwhatsapp.com%2Fchannel%2F0029Va", nil))

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, response.StatusCode)
	require.NotNil(t, service.infoRequest)
	assert.Equal(t, "https://whatsapp.com/channel/0029Va", service.infoRequest.Invite)
	assert.Empty(t, service.infoRequest.NewsletterID)
}

func TestNewsletterDirectoryRouteBindsFilters(t *testing.T) {
	service := &newsletterServiceStub{}
	app := newNewsletterTestApp(service, whatsapp.NewDeviceInstance("device-a", nil, nil))

	response, err := app.Test(httptest.NewRequest("GET", "/newsletter/directory?view=TRENDING&limit=10&country_codes=BR&country_codes=ID&search_text=news", nil))

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, response.StatusCode)
	require.NotNil(t, service.directoryRequest)
	assert.Equal(t, "TRENDING", service.directoryRequest.View)
	assert.Equal(t, 10, service.directoryRequest.Limit)
	assert.Equal(t, "news", service.directoryRequest.SearchText)
	assert.Equal(t, []string{"BR", "ID"}, service.directoryRequest.CountryCodes)
}

func TestNewsletterMessageUpdatesRoute(t *testing.T) {
	service := &newsletterServiceStub{}
	app := newNewsletterTestApp(service, whatsapp.NewDeviceInstance("device-a", nil, nil))

	response, err := app.Test(httptest.NewRequest("GET", "/newsletter/message-updates?newsletter_id=120363123456789%40newsletter&count=20&since=1761650000&after=100", nil))

	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, response.StatusCode)
	require.NotNil(t, service.updatesRequest)
	assert.Equal(t, 20, service.updatesRequest.Count)
	assert.Equal(t, int64(1761650000), service.updatesRequest.Since)
	assert.Equal(t, 100, service.updatesRequest.After)
}

func TestNewsletterLiveUpdatesRoutes(t *testing.T) {
	service := &newsletterServiceStub{}
	app := newNewsletterTestApp(service, whatsapp.NewDeviceInstance("device-a", nil, nil))

	request := httptest.NewRequest("POST", "/newsletter/live-updates", strings.NewReader(`{"newsletter_id":"120363123456789@newsletter"}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, response.StatusCode)
	require.NotNil(t, service.liveRequest)
	assert.Equal(t, "120363123456789@newsletter", service.liveRequest.NewsletterID)

	service.liveRequest = nil
	response, err = app.Test(httptest.NewRequest("DELETE", "/newsletter/live-updates?newsletter_id=120363123456789%40newsletter", nil))
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, response.StatusCode)
	require.NotNil(t, service.liveRequest)
	assert.Equal(t, "120363123456789@newsletter", service.liveRequest.NewsletterID)
}
