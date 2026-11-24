package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// remount rebuilds e.app after a change to e.d (Mount captures Deps by value).
func remount(e *env, stub *devStub) {
	e.d.Devices = owners.NewScopedDevices(stub, e.st)
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	app.Use(e.d.Auth.Middleware())
	Mount(app, e.d)
	app.Use(e.d.Auth.RequireAPI())
	e.app = app
}

func getPage(t *testing.T, e *env, ck *http.Cookie, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(ck)
	_, body := e.do(t, req)
	return body
}

func TestSuperadminSeesSignInIsDownWhenTheServiceNumberIsNotConnected(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_svc", PhoneNumber: "27825550199", State: domainDevice.DeviceStateDisconnected}}}
	e := withDevices(t, stub)
	admin, adminID := signedIn(t, e, "+27825550100")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_svc", adminID))
	require.NoError(t, e.st.SetSetting(context.Background(), store.SettingServiceDeviceID, "d_svc", adminID))
	e.d.ServicePhone = func(context.Context) string { return "" }
	remount(e, stub)

	home := getPage(t, e, admin, "/")
	require.Contains(t, home, "Sign-in is down: the sign-in number isn")
	require.Contains(t, home, "This is the sign-in number. Nobody will be able to sign in by WhatsApp.", "the unlink confirm warns")
	settings := getPage(t, e, admin, "/admin/settings")
	require.Contains(t, settings, "Sign-in is down")
	require.Contains(t, settings, "Not connected")

	// A plain member never sees it.
	member, memberID := signedIn(t, e, "+27825550101")
	_ = memberID
	require.NotContains(t, getPage(t, e, member, "/"), "Sign-in is down")
}

func TestSuperadminIsOfferedAConnectedNumberAsTheSignInNumberWhenNoneIsSet(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_ok", PhoneNumber: "27825550199", State: domainDevice.DeviceStateLoggedIn}}}
	e := withDevices(t, stub)
	admin, adminID := signedIn(t, e, "+27825550100")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_ok", adminID))

	home := getPage(t, e, admin, "/")
	require.Contains(t, home, "Use as sign-in number")
	require.Contains(t, home, `action="/admin/settings/service-number"`)
	require.Contains(t, home, `name="device_id" value="d_ok"`)
	require.Contains(t, getPage(t, e, admin, "/admin/settings"), "Choose a number")

	require.NoError(t, e.st.SetSetting(context.Background(), store.SettingServiceDeviceID, "d_ok", adminID))
	home = getPage(t, e, admin, "/")
	require.NotContains(t, home, "Use as sign-in number", "only while none is set")
	require.Contains(t, home, "Sign-in number")
}

func TestHomeNudgesTowardsAPasskeyUntilThereIsOne(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	home := getPage(t, e, ck, "/")
	require.Contains(t, home, "Sign in faster next time — add a passkey")
	require.Contains(t, home, "data-passkey-register")
	require.Contains(t, home, `data-dismiss="passkey"`)

	require.NoError(t, e.st.AddPasskey(context.Background(), store.Passkey{CredentialID: "c1", UserID: me, Name: "Safari on iOS", CredentialJSON: []byte(`{}`)}))
	require.NotContains(t, getPage(t, e, ck, "/"), "add a passkey")
}
