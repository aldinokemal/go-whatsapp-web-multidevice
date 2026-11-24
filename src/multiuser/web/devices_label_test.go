package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// withLabelRoutes mounts the pages plus both label routes the way Task 8
// wires them: the page route with the pages, the JSON route behind the API
// gate (cookie or API key).
func withLabelRoutes(t *testing.T, stub *devStub) *env {
	e := newEnv(t)
	e.d.Devices = owners.NewScopedDevices(stub, e.st)
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	app.Use(e.d.Auth.Middleware())
	Mount(app, e.d)
	MountDeviceLabelPage(app, e.d)
	app.Use(e.d.Auth.APIKey(), e.d.Auth.RequireAPI(), e.d.Auth.RequireSameOriginAPI(), e.d.Auth.VerifyCSRF())
	MountDeviceLabelAPI(app, e.d)
	e.app = app
	return e
}

func postLabelPage(t *testing.T, e *env, ck *http.Cookie, csrf, id, label string, htmx bool) *http.Response {
	t.Helper()
	form := url.Values{"label": {label}, "csrf": {csrf}}
	req := httptest.NewRequest(http.MethodPost, "/ui/devices/"+id+"/label", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	return resp
}

func putLabelAPI(t *testing.T, e *env, bearer, id, label string) (*http.Response, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"label": label})
	req := httptest.NewRequest(http.MethodPut, "/devices/"+id+"/label", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	return e.do(t, req)
}

// Behaviour: the owner names a number from the page and from the API, and
// the name shows up in the device list both read.
func TestLabelRoutesNameMyNumber(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}}}
	e := withLabelRoutes(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	csrf := csrfFor(t, e, ck)

	resp := postLabelPage(t, e, ck, csrf, "d_mine", "  Work   phone ", false)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	labels, err := e.st.LabelsOf(context.Background(), me)
	require.NoError(t, err)
	require.Equal(t, "Work phone", labels["d_mine"])

	resp = postLabelPage(t, e, ck, csrf, "d_mine", "Home", true)
	require.Equal(t, http.StatusOK, resp.StatusCode, "htmx gets the row back, swapped in place")

	key, _, err := e.st.CreateAPIKey(context.Background(), me, "script")
	require.NoError(t, err)
	resp, body := putLabelAPI(t, e, key, "d_mine", "Shop")
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	require.Contains(t, body, `"label":"Shop"`)

	list, err := e.d.Devices.ListDevices(userCtx(t, e, me))
	require.NoError(t, err)
	require.Equal(t, "Shop", list[0].Label)

	resp, body = putLabelAPI(t, e, key, "d_mine", "")
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	labels, _ = e.st.LabelsOf(context.Background(), me)
	require.Equal(t, "", labels["d_mine"], "an empty name clears it")
}

// Security: another user's number is a 404 on both routes, and stays unnamed.
func TestLabelAnotherUsersDeviceIs404(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}, {ID: "d_theirs"}}}
	e := withLabelRoutes(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	require.NoError(t, e.st.SetOwner(context.Background(), "d_theirs", them))
	csrf := csrfFor(t, e, ck)
	key, _, err := e.st.CreateAPIKey(context.Background(), me, "script")
	require.NoError(t, err)

	require.Equal(t, http.StatusNotFound, postLabelPage(t, e, ck, csrf, "d_theirs", "mine now", false).StatusCode)
	require.Equal(t, http.StatusNotFound, postLabelPage(t, e, ck, csrf, "d_nope", "x", false).StatusCode)
	resp, body := putLabelAPI(t, e, key, "d_theirs", "mine now")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.NotContains(t, body, "d_theirs")

	labels, err := e.st.LabelsOf(context.Background(), them)
	require.NoError(t, err)
	require.Equal(t, "", labels["d_theirs"])
}

// Behaviour: a bad name is refused with a reason, on both routes.
func TestLabelValidationErrors(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}}}
	e := withLabelRoutes(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	csrf := csrfFor(t, e, ck)
	key, _, err := e.st.CreateAPIKey(context.Background(), me, "script")
	require.NoError(t, err)

	resp := postLabelPage(t, e, ck, csrf, "d_mine", strings.Repeat("a", 41), false)
	require.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	resp, body := putLabelAPI(t, e, key, "d_mine", "‮enohp")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, body, "VALIDATION_ERROR")
	labels, _ := e.st.LabelsOf(context.Background(), me)
	require.Equal(t, "", labels["d_mine"])
}

// Security: the page route needs the CSRF token.
func TestLabelPageNeedsCSRF(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}}}
	e := withLabelRoutes(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))

	resp := postLabelPage(t, e, ck, "wrong", "d_mine", "Work", false)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	labels, _ := e.st.LabelsOf(context.Background(), me)
	require.Equal(t, "", labels["d_mine"])
}

func userCtx(t *testing.T, e *env, userID string) context.Context {
	t.Helper()
	u, err := e.st.UserByID(context.Background(), userID)
	require.NoError(t, err)
	return auth.WithUser(context.Background(), u)
}

// Security: a name is text. In JSON it comes back with its markup escaped,
// so a client that drops it into HTML unescaped still gets no tag. (The
// page rendering is html/template's job; Task 7 pins it on the row.)
func TestLabelIsEscapedInJSON(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}}}
	e := withLabelRoutes(t, stub)
	_, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	key, _, err := e.st.CreateAPIKey(context.Background(), me, "script")
	require.NoError(t, err)

	resp, body := putLabelAPI(t, e, key, "d_mine", "<script>x</script>")
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	require.NotContains(t, body, "<script>")
	labels, _ := e.st.LabelsOf(context.Background(), me)
	require.Equal(t, "<script>x</script>", labels["d_mine"], "stored as typed; escaping happens on output")
}
