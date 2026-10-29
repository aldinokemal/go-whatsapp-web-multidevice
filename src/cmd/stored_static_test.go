package cmd

import (
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestStoredDocumentsAreAttachmentsAndUIStillServesHTML(t *testing.T) {
	root := t.TempDir()
	prevRoot := storedStaticRoot
	prevUI := config.AppUIEnabled
	prevUpdate := config.AppUIAutoUpdate
	prevBase := config.AppBasePath
	prevCache := config.PathUICache
	storedStaticRoot = root
	config.AppUIEnabled = true
	config.AppUIAutoUpdate = false
	config.AppBasePath = ""
	config.PathUICache = t.TempDir()
	t.Cleanup(func() {
		storedStaticRoot = prevRoot
		config.AppUIEnabled = prevUI
		config.AppUIAutoUpdate = prevUpdate
		config.AppBasePath = prevBase
		config.PathUICache = prevCache
	})

	htmlBody := []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "media"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "qrcode"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "media", "note.html"), htmlBody, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "media", "photo.png.html"), htmlBody, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "media", "icon.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`), 0o644))
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D}
	require.NoError(t, os.WriteFile(filepath.Join(root, "qrcode", "scan.png"), png, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "media", "pic.png"), png, 0o644))

	app := fiber.New()
	registerStoredStatic(app)
	registerUIRoute(app, t.Context())

	htmlResp := mustGet(t, app, "/statics/media/note.html")
	require.Equal(t, fiber.StatusOK, htmlResp.StatusCode)
	require.Equal(t, "application/octet-stream", htmlResp.Header.Get(fiber.HeaderContentType))
	require.Contains(t, htmlResp.Header.Get(fiber.HeaderContentDisposition), "attachment")
	require.Equal(t, "nosniff", htmlResp.Header.Get(fiber.HeaderXContentTypeOptions))
	require.NotContains(t, htmlResp.Header.Get(fiber.HeaderContentType), "text/html")

	disguised := mustGet(t, app, "/statics/media/photo.png.html")
	require.Equal(t, "application/octet-stream", disguised.Header.Get(fiber.HeaderContentType))
	require.Contains(t, disguised.Header.Get(fiber.HeaderContentDisposition), "attachment")

	svgResp := mustGet(t, app, "/statics/media/icon.svg")
	require.Equal(t, "application/octet-stream", svgResp.Header.Get(fiber.HeaderContentType))
	require.Contains(t, svgResp.Header.Get(fiber.HeaderContentDisposition), "attachment")

	qrResp := mustGet(t, app, "/statics/qrcode/scan.png")
	require.Equal(t, fiber.StatusOK, qrResp.StatusCode)
	require.Equal(t, "image/png", qrResp.Header.Get(fiber.HeaderContentType))
	require.Equal(t, "inline", qrResp.Header.Get(fiber.HeaderContentDisposition))
	require.Equal(t, "nosniff", qrResp.Header.Get(fiber.HeaderXContentTypeOptions))

	imageResp := mustGet(t, app, "/statics/media/pic.png")
	require.Equal(t, "image/png", imageResp.Header.Get(fiber.HeaderContentType))
	require.Equal(t, "inline", imageResp.Header.Get(fiber.HeaderContentDisposition))

	uiResp := mustGet(t, app, "/")
	require.Equal(t, fiber.StatusOK, uiResp.StatusCode)
	require.Contains(t, uiResp.Header.Get(fiber.HeaderContentType), "text/html")
	uiBody, err := io.ReadAll(uiResp.Body)
	require.NoError(t, err)
	require.Contains(t, string(uiBody), "gowa")
	require.Contains(t, string(uiBody), "dashboard has not been downloaded yet")
}

func TestStoredStaticEscapedNamesAndConditionalRequests(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "statics")
	prevRoot := storedStaticRoot
	prevBase := config.AppBasePath
	storedStaticRoot = root
	config.AppBasePath = ""
	t.Cleanup(func() {
		storedStaticRoot = prevRoot
		config.AppBasePath = prevBase
	})

	pdf := []byte("%PDF-1.4\n%gowa\n")
	names := []string{"1700-uuid.final draft", "отчёт.pdf", "v1#2.pdf"}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "media"), 0o755))
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(root, "media", name), pdf, 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(base, "storages"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "storages", "whatsapp.db"), []byte("secret"), 0o644))

	app := fiber.New()
	registerStoredStatic(app)

	// Stored file URLs escape each path segment.
	for _, name := range names {
		resp := mustGet(t, app, "/statics/media/"+url.PathEscape(name))
		require.Equal(t, fiber.StatusOK, resp.StatusCode, name)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, pdf, body)
	}
	unicodeResp := mustGet(t, app, "/statics/media/"+url.PathEscape("отчёт.pdf"))
	require.Contains(t, unicodeResp.Header.Get(fiber.HeaderContentDisposition), "filename*=UTF-8''"+url.PathEscape("отчёт.pdf"))

	head, err := app.Test(httptest.NewRequest(fiber.MethodHead, "/statics/media/v1%232.pdf", nil))
	require.NoError(t, err)
	require.NoError(t, head.Body.Close())
	require.Equal(t, fiber.StatusOK, head.StatusCode)
	require.Equal(t, strconv.Itoa(len(pdf)), head.Header.Get(fiber.HeaderContentLength))
	lastModified := head.Header.Get(fiber.HeaderLastModified)
	require.NotEmpty(t, lastModified)

	conditional := httptest.NewRequest(fiber.MethodGet, "/statics/media/v1%232.pdf", nil)
	conditional.Header.Set(fiber.HeaderIfModifiedSince, lastModified)
	cached, err := app.Test(conditional)
	require.NoError(t, err)
	require.NoError(t, cached.Body.Close())
	require.Equal(t, fiber.StatusNotModified, cached.StatusCode)

	for _, target := range []string{
		"/statics/%2e%2e/storages/whatsapp.db",
		"/statics/..%2fstorages%2fwhatsapp.db",
		"/statics/media/..%2f..%2fstorages/whatsapp.db",
		"/statics/%252e%252e/storages/whatsapp.db",
		"/statics/media/v1%232.pdf%00.png",
		"/statics/media%5c..%5c..%5cstorages%5cwhatsapp.db",
	} {
		require.Equal(t, fiber.StatusNotFound, mustGet(t, app, target).StatusCode, target)
	}
	_, ok := storedStaticRelative("/statics/media/%zz")
	require.False(t, ok)
}

func mustGet(t *testing.T, app *fiber.App, path string) *httpResponse {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, path, nil))
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return &httpResponse{resp.StatusCode, resp.Header, resp.Body}
}

type httpResponse struct {
	StatusCode int
	Header     interface{ Get(string) string }
	Body       io.ReadCloser
}
