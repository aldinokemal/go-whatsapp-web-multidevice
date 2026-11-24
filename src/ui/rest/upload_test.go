package rest

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/upload"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var onePixelPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

func newUploadTestApp(t *testing.T) (*fiber.App, *upload.Store) {
	t.Helper()
	store := upload.NewStore(t.TempDir())
	// Mirrors cmd/rest.go's own fiber.Config{BodyLimit: ...}: without it,
	// fiber's smaller built-in default would reject a body this test sends
	// before InitRestUpload's own size check ever ran.
	app := fiber.New(fiber.Config{BodyLimit: int(config.WhatsappSettingMaxVideoSize)})
	InitRestUpload(app, store)
	return app, store
}

func putUpload(t *testing.T, app *fiber.App, id string, body []byte) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/uploads/"+id, bytes.NewReader(body))
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

// getUpload fetches via the real shape (/file.jpg) create_upload actually
// hands out - see getUploadAt for fetching via a different trailing segment.
func getUpload(t *testing.T, app *fiber.App, id string) *http.Response {
	t.Helper()
	return getUploadAt(t, app, "/uploads/"+id+"/file.jpg")
}

func getUploadAt(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func TestUploadRouteRoundTrips(t *testing.T) {
	app, store := newUploadTestApp(t)
	id, err := store.Create("device-1", time.Minute, config.WhatsappSettingMaxImageSize)
	require.NoError(t, err)

	putResp := putUpload(t, app, id, onePixelPNG)
	assert.Equal(t, http.StatusOK, putResp.StatusCode)

	getResp := getUpload(t, app, id)
	assert.Equal(t, http.StatusOK, getResp.StatusCode)
	assert.Equal(t, "image/png", getResp.Header.Get("Content-Type"))
	got, readErr := io.ReadAll(getResp.Body)
	require.NoError(t, readErr)
	assert.Equal(t, onePixelPNG, got)
}

// TestUploadRouteGetIgnoresTheURLsTrailingSegment guards a real bug a live
// end-to-end test caught: WTF's own image_url downloader derives a file
// extension from the URL's own last path segment and refuses anything that
// isn't jpg/png/webp there - create_upload always names a fixed ".jpg" for
// that reason, since the real format isn't known until after the PUT. The
// GET route must therefore serve correctly by ticket id alone, whatever
// name shows up after it.
func TestUploadRouteGetIgnoresTheURLsTrailingSegment(t *testing.T) {
	app, store := newUploadTestApp(t)
	id, err := store.Create("device-1", time.Minute, config.WhatsappSettingMaxImageSize)
	require.NoError(t, err)
	require.NoError(t, store.Upload(id, onePixelPNG))

	for _, suffix := range []string{"file.jpg", "whatever.png", "no-extension-at-all"} {
		resp := getUploadAt(t, app, "/uploads/"+id+"/"+suffix)
		assert.Equal(t, http.StatusOK, resp.StatusCode, "suffix %q", suffix)
		assert.Equal(t, "image/png", resp.Header.Get("Content-Type"), "suffix %q", suffix)
	}
}

func TestUploadRoutePutUnknownIDIs404(t *testing.T) {
	app, _ := newUploadTestApp(t)
	resp := putUpload(t, app, "no-such-ticket", onePixelPNG)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestUploadRouteGetBeforePutIs404(t *testing.T) {
	app, store := newUploadTestApp(t)
	id, err := store.Create("device-1", time.Minute, config.WhatsappSettingMaxImageSize)
	require.NoError(t, err)

	resp := getUpload(t, app, id)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestUploadRouteSecondPutIs409(t *testing.T) {
	app, store := newUploadTestApp(t)
	id, err := store.Create("device-1", time.Minute, config.WhatsappSettingMaxImageSize)
	require.NoError(t, err)

	first := putUpload(t, app, id, onePixelPNG)
	require.Equal(t, http.StatusOK, first.StatusCode)

	second := putUpload(t, app, id, onePixelPNG)
	assert.Equal(t, http.StatusConflict, second.StatusCode)
}

func TestUploadRouteUnsupportedTypeIs415(t *testing.T) {
	app, store := newUploadTestApp(t)
	id, err := store.Create("device-1", time.Minute, config.WhatsappSettingMaxImageSize)
	require.NoError(t, err)

	resp := putUpload(t, app, id, []byte("#!/bin/sh\necho not an image\n"))
	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
}

func TestUploadRouteOverTicketCapIs413(t *testing.T) {
	app, store := newUploadTestApp(t)
	id, err := store.Create("device-1", time.Minute, int64(len(onePixelPNG)-1))
	require.NoError(t, err)

	resp := putUpload(t, app, id, onePixelPNG)
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestUploadRouteOverTheGlobalCapIs413(t *testing.T) {
	app, store := newUploadTestApp(t)
	id, err := store.Create("device-1", time.Minute, config.WhatsappSettingMaxImageSize)
	require.NoError(t, err)

	huge := make([]byte, config.WhatsappSettingMaxImageSize+1)
	resp := putUpload(t, app, id, huge)
	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}
