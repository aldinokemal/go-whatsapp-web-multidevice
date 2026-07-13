package cmd

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
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

func TestCORSPreflightAllowsUIHeaders(t *testing.T) {
	app := fiber.New()
	app.Use(newCORSMiddleware())
	app.Get("/app/info", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest("OPTIONS", "/app/info", nil)
	req.Header.Set("Origin", "https://ui.example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "authorization,x-device-id")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
	allowedHeaders := strings.ToLower(resp.Header.Get("Access-Control-Allow-Headers"))
	assert.Contains(t, allowedHeaders, "authorization")
	assert.Contains(t, allowedHeaders, "x-device-id")
}

func TestWebsocketQueryAuth(t *testing.T) {
	validQuery := base64.StdEncoding.EncodeToString([]byte("user:secret"))
	wrongQuery := base64.StdEncoding.EncodeToString([]byte("user:wrong"))

	newApp := func() *fiber.App {
		app := fiber.New()
		app.Use(middleware.WebsocketQueryAuth())
		app.Use(newBasicAuthMiddleware(map[string]string{"user": "secret"}))
		app.Get("/ws", func(c fiber.Ctx) error {
			return c.SendStatus(fiber.StatusOK)
		})
		return app
	}

	setUpgradeHeaders := func(req *http.Request) {
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
	}

	tests := []struct {
		name       string
		query      string
		upgrade    bool
		wantStatus int
	}{
		{
			name:       "upgrade with valid query credentials passes",
			query:      "?authorization=" + validQuery,
			upgrade:    true,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "upgrade with wrong query credentials is rejected",
			query:      "?authorization=" + wrongQuery,
			upgrade:    true,
			wantStatus: fiber.StatusUnauthorized,
		},
		{
			name:       "upgrade without credentials is rejected",
			query:      "",
			upgrade:    true,
			wantStatus: fiber.StatusUnauthorized,
		},
		{
			name:       "non-upgrade request ignores query credentials",
			query:      "?authorization=" + validQuery,
			upgrade:    false,
			wantStatus: fiber.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws"+tt.query, nil)
			if tt.upgrade {
				setUpgradeHeaders(req)
			}

			resp, err := newApp().Test(req)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStatus, resp.StatusCode)
		})
	}

	t.Run("plus signs decoded as spaces are restored", func(t *testing.T) {
		app := fiber.New()
		app.Use(middleware.WebsocketQueryAuth())
		app.Get("/ws", func(c fiber.Ctx) error {
			return c.SendString(string(c.Request().Header.Peek(fiber.HeaderAuthorization)))
		})

		req := httptest.NewRequest("GET", "/ws?authorization=AB+CD", nil)
		setUpgradeHeaders(req)

		resp, err := app.Test(req)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "Basic AB+CD", string(body))
	})
}

func TestAppInfo(t *testing.T) {
	t.Run("returns server metadata", func(t *testing.T) {
		app := fiber.New()
		rest.InitRestAppInfo(app)

		resp, err := app.Test(httptest.NewRequest("GET", "/app/info", nil))
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)

		var payload struct {
			Code    string         `json:"code"`
			Results map[string]any `json:"results"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
		assert.Equal(t, "SUCCESS", payload.Code)
		assert.Equal(t, config.AppVersion, payload.Results["version"])
		assert.EqualValues(t, config.WhatsappSettingMaxFileSize, payload.Results["max_file_size"])
		assert.EqualValues(t, config.WhatsappSettingMaxVideoSize, payload.Results["max_video_size"])
		assert.EqualValues(t, config.WhatsappSettingMaxImageSize, payload.Results["max_image_size"])
	})

	t.Run("requires credentials when basic auth is enabled", func(t *testing.T) {
		app := fiber.New()
		app.Use(newBasicAuthMiddleware(map[string]string{"user": "secret"}))
		rest.InitRestAppInfo(app)

		resp, err := app.Test(httptest.NewRequest("GET", "/app/info", nil))
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)

		authed := httptest.NewRequest("GET", "/app/info", nil)
		authed.SetBasicAuth("user", "secret")
		resp, err = app.Test(authed)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	})
}
