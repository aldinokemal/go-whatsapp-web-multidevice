package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type devStub struct {
	devices  []domainDevice.Device
	qrPath   string
	loggedIn bool
}

func (s *devStub) ListDevices(context.Context) ([]domainDevice.Device, error) { return s.devices, nil }
func (s *devStub) GetDevice(_ context.Context, id string) (*domainDevice.Device, error) {
	return &domainDevice.Device{ID: id}, nil
}
func (s *devStub) AddDevice(_ context.Context, id string, _ *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	s.devices = append(s.devices, domainDevice.Device{ID: id})
	return &domainDevice.Device{ID: id}, nil
}
func (s *devStub) RemoveDevice(context.Context, string) error { return nil }
func (s *devStub) LoginDevice(context.Context, string) (domainApp.LoginResponse, error) {
	return domainApp.LoginResponse{ImagePath: s.qrPath, Duration: 20}, nil
}
func (s *devStub) LoginDeviceWithCode(context.Context, string, string) (string, error) {
	return "ABCD-EFGH", nil
}
func (s *devStub) LogoutDevice(context.Context, string) error    { return nil }
func (s *devStub) ReconnectDevice(context.Context, string) error { return nil }
func (s *devStub) GetStatus(context.Context, string) (bool, bool, error) {
	return s.loggedIn, s.loggedIn, nil
}
func (s *devStub) SetDeviceWebhook(context.Context, string, string) error   { return nil }
func (s *devStub) GetDeviceWebhook(context.Context, string) (string, error) { return "", nil }
func (s *devStub) SetDeviceWebhookConfig(context.Context, string, *chatstorage.DeviceWebhookConfig) error {
	return nil
}
func (s *devStub) GetDeviceWebhookConfig(context.Context, string) (*chatstorage.DeviceWebhookConfig, error) {
	return nil, nil
}

func signedIn(t *testing.T, e *env, phone string) (*http.Cookie, string) {
	t.Helper()
	u, err := e.st.CreateUser(context.Background(), phone)
	require.NoError(t, err)
	raw, err := e.st.CreateLoginToken(context.Background(), phone, 15*time.Minute)
	require.NoError(t, err)
	form := url.Values{"t": {raw}}
	req := httptest.NewRequest(http.MethodPost, "/signin/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := e.do(t, req)
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return c, u.ID
		}
	}
	t.Fatal("no cookie")
	return nil, ""
}

// csrfFrom pulls the CSRF token out of the hidden sign-out field layout.html
// renders on every authenticated page, since this package exposes no way to
// compute one directly from a raw session string.
func csrfFrom(t *testing.T, body string) string {
	t.Helper()
	const marker = `name="csrf" value="`
	i := strings.Index(body, marker)
	require.GreaterOrEqual(t, i, 0, "csrf token not found in page body")
	rest := body[i+len(marker):]
	j := strings.Index(rest, `"`)
	require.Greater(t, j, 0)
	return rest[:j]
}

func csrfFor(t *testing.T, e *env, ck *http.Cookie) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(ck)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	return csrfFrom(t, body)
}

func withDevices(t *testing.T, stub *devStub) *env {
	e := newEnv(t)
	e.d.Devices = owners.NewScopedDevices(stub, e.st)
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	app.Use(e.d.Auth.Middleware())
	Mount(app, e.d)
	app.Use(e.d.Auth.RequireAPI())
	e.app = app
	return e
}

func TestHomeListsOnlyMyDevicesAndQRIsOwnerChecked(t *testing.T) {
	qr := filepath.Join(t.TempDir(), "scan-qr.png")
	require.NoError(t, os.WriteFile(qr, []byte("\x89PNG fake"), 0o600))
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}, {ID: "d_theirs"}}, qrPath: qr}
	e := withDevices(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	require.NoError(t, e.st.SetOwner(context.Background(), "d_theirs", them))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(ck)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "d_mine")
	require.NotContains(t, body, "d_theirs")
	require.Contains(t, body, "Link WhatsApp")

	req = httptest.NewRequest(http.MethodGet, "/ui/devices/d_theirs/qr.png", nil)
	req.AddCookie(ck)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	req = httptest.NewRequest(http.MethodGet, "/ui/devices/d_mine/qr.png", nil)
	req.AddCookie(ck)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "image/png", resp.Header.Get("Content-Type"))
}

func TestStatusRefreshesWhenLinked(t *testing.T) {
	stub := &devStub{}
	e := withDevices(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	req := httptest.NewRequest(http.MethodGet, "/ui/devices/d_mine/status", nil)
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	stub.loggedIn = true
	resp, _ = e.do(t, req.Clone(context.Background()))
	require.Equal(t, "true", resp.Header.Get("HX-Refresh"))
}

func TestCreateDeviceNeedsCSRF(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, _ := signedIn(t, e, "+27825550101")
	req := httptest.NewRequest(http.MethodPost, "/ui/devices", nil)
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCreateDeviceWithCSRFOpensLinkModal(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	csrf := csrfFor(t, e, ck)
	req := httptest.NewRequest(http.MethodPost, "/ui/devices", nil)
	req.AddCookie(ck)
	req.Header.Set("X-CSRF-Token", csrf)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "QR code to link WhatsApp")

	ids, err := e.st.DevicesOf(context.Background(), me)
	require.NoError(t, err)
	require.Len(t, ids, 1)
}

func TestUnlinkRemovesDeviceAndOwnership(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}}}
	e := withDevices(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	csrf := csrfFor(t, e, ck)

	form := url.Values{"csrf": {csrf}}
	req := httptest.NewRequest(http.MethodPost, "/ui/devices/d_mine/unlink", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/", resp.Header.Get("Location"))

	_, err := e.st.OwnerOf(context.Background(), "d_mine")
	require.Error(t, err)
}

func TestUnlinkSomeoneElsesDeviceIsNotFound(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_theirs"}}}
	e := withDevices(t, stub)
	ck, _ := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_theirs", them))
	csrf := csrfFor(t, e, ck)

	form := url.Values{"csrf": {csrf}}
	req := httptest.NewRequest(http.MethodPost, "/ui/devices/d_theirs/unlink", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestReconnectRedirectsHome(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}}}
	e := withDevices(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	csrf := csrfFor(t, e, ck)

	form := url.Values{"csrf": {csrf}}
	req := httptest.NewRequest(http.MethodPost, "/ui/devices/d_mine/reconnect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/", resp.Header.Get("Location"))
}

func TestPairCodeFragmentShowsCode(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}}}
	e := withDevices(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	csrf := csrfFor(t, e, ck)

	form := url.Values{"csrf": {csrf}, "phone": {"+27825550199"}}
	req := httptest.NewRequest(http.MethodPost, "/ui/devices/d_mine/pair-code", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "ABCD-EFGH")
}

func TestEmptyStateShowsNoDevices(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, _ := signedIn(t, e, "+27825550101")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(ck)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "Link your WhatsApp to get started.")
}

// An unlinked device (its slot reserved, never scanned) can have its link
// dialog re-opened without creating a second device, and that reopen is
// ownership-checked exactly like every other device route.
func TestLinkRouteReopensExistingDeviceOwnerChecked(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}, {ID: "d_theirs"}}}
	e := withDevices(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	require.NoError(t, e.st.SetOwner(context.Background(), "d_theirs", them))

	req := httptest.NewRequest(http.MethodGet, "/ui/devices/d_mine/link", nil)
	req.AddCookie(ck)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "QR code to link WhatsApp")

	req = httptest.NewRequest(http.MethodGet, "/ui/devices/d_theirs/link", nil)
	req.AddCookie(ck)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
