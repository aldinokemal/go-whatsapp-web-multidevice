package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestDeviceResolveFuncOverridesManager(t *testing.T) {
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	_, err := dm.CreateDevice(t.Context(), "upstream-default")
	require.NoError(t, err)

	saved := DeviceResolveFunc
	t.Cleanup(func() { DeviceResolveFunc = saved })
	var asked string
	DeviceResolveFunc = func(_ fiber.Ctx, id string) (*whatsapp.DeviceInstance, string, error) {
		asked = id
		return nil, id, pkgError.ErrDeviceNotFound
	}

	app := fiber.New()
	app.Get("/x", DeviceMiddleware(dm), func(c fiber.Ctx) error { return c.SendString("reached") })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(DeviceIDHeader, "someone-elses")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, "someone-elses", asked)

	// With no header the hook decides too: upstream's "only device" fallback is never used.
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/x", nil))
	require.NoError(t, err)
	require.NotEqual(t, http.StatusOK, resp.StatusCode)
}

// The multi-user resolver's ownership errors must reach the caller with their own
// message, not the generic DEVICE_ID_REQUIRED text.
func TestDeviceResolveFuncPassesOwnershipErrorMessages(t *testing.T) {
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	saved := DeviceResolveFunc
	t.Cleanup(func() { DeviceResolveFunc = saved })

	for _, ownErr := range []error{owners.ErrNoDeviceLinked, owners.ErrDeviceIDRequired} {
		DeviceResolveFunc = func(fiber.Ctx, string) (*whatsapp.DeviceInstance, string, error) {
			return nil, "", ownErr
		}
		app := fiber.New()
		app.Get("/x", DeviceMiddleware(dm), func(c fiber.Ctx) error { return c.SendString("reached") })

		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/x", nil))
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		var got utils.ResponseData
		require.NoError(t, json.Unmarshal(body, &got))
		require.Equal(t, ownErr.Error(), got.Message)
		require.Equal(t, "VALIDATION_ERROR", got.Code)
	}
}

// With no hook installed the middleware is upstream's: the only device is the default.
func TestDeviceResolveFuncNilKeepsUpstreamBehaviour(t *testing.T) {
	saved := DeviceResolveFunc
	t.Cleanup(func() { DeviceResolveFunc = saved })
	DeviceResolveFunc = nil

	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	app := fiber.New()
	app.Get("/x", DeviceMiddleware(dm), func(c fiber.Ctx) error { return c.SendString("reached") })

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/x", nil))
	require.NoError(t, err)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "DEVICE_ID_REQUIRED")

	_, err = dm.CreateDevice(t.Context(), "only")
	require.NoError(t, err)
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/x", nil))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
