package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

// postForm is shared by every admin test below. csrfFor and signedIn already
// exist in devices_test.go (Task 10), so this file reuses them rather than
// redefining its own copies, which is a deliberate departure from the plan's
// own sample admin_test.go: that sample assumed neither helper existed yet,
// but by Task 11 both already do (csrfFor there fetches "/", which any
// signed-in user, superadmin or not, can reach). It also returns the body
// alongside the response, unlike the plan's own sample, which read
// resp.Body a second time after e.do had already drained and closed it -
// the second read always came back empty.
func postForm(t *testing.T, e *env, ck *http.Cookie, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	return e.do(t, req)
}

func TestAdminPagesAreSuperadminOnly(t *testing.T) {
	e := withDevices(t, &devStub{})
	plain, _ := signedIn(t, e, "+27825550101")
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.AddCookie(plain)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html", "friendly page, not JSON")
	require.Contains(t, body, "couldn&#39;t find that page")

	req = httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	req.AddCookie(plain)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAdminUsersListsEveryoneWithCountAndLinkedPhones(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, adminID := signedIn(t, e, "+27825550100")
	_, otherID := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_1", otherID))
	require.NoError(t, e.st.SetOwner(context.Background(), "d_2", otherID))

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.AddCookie(admin)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "2 users")
	require.Contains(t, body, "&#43;27 82 555 0100", "html/template escapes '+'; numbers are shown grouped")
	require.Contains(t, body, "&#43;27 82 555 0101")
	require.Contains(t, body, "You", "the caller's own row is marked")
	_ = adminID
}

func TestDisablingAUserEndsTheirSessions(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	victim, victimID := signedIn(t, e, "+27825550101")
	tok := csrfFor(t, e, admin)

	resp, _ := postForm(t, e, admin, "/admin/users/"+victimID+"/status", url.Values{"csrf": {tok}, "status": {"disabled"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(victim)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "disabled user bounced to sign-in")
	u, _ := e.st.UserByID(context.Background(), victimID)
	require.Equal(t, store.StatusDisabled, u.Status)
}

func TestDisablingAUserNeedsCSRF(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, victimID := signedIn(t, e, "+27825550101")
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+victimID+"/status",
		strings.NewReader(url.Values{"status": {"disabled"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(admin)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestEnablingADisabledUserWorks(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, victimID := signedIn(t, e, "+27825550101")
	tok := csrfFor(t, e, admin)
	_, _ = postForm(t, e, admin, "/admin/users/"+victimID+"/status", url.Values{"csrf": {tok}, "status": {"disabled"}})

	resp, _ := postForm(t, e, admin, "/admin/users/"+victimID+"/status", url.Values{"csrf": {tok}, "status": {"active"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	u, _ := e.st.UserByID(context.Background(), victimID)
	require.Equal(t, store.StatusActive, u.Status)
}

func TestSuperadminCannotDisableThemselves(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, adminID := signedIn(t, e, "+27825550100")
	tok := csrfFor(t, e, admin)
	resp, _ := postForm(t, e, admin, "/admin/users/"+adminID+"/status", url.Values{"csrf": {tok}, "status": {"disabled"}})
	require.NotEqual(t, http.StatusSeeOther, resp.StatusCode)
	u, _ := e.st.UserByID(context.Background(), adminID)
	require.Equal(t, store.StatusActive, u.Status)
}

func TestServiceNumberMustBeTheSuperadminsOwnDevice(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, adminID := signedIn(t, e, "+27825550100")
	_, otherID := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_admin", adminID))
	require.NoError(t, e.st.SetOwner(context.Background(), "d_other", otherID))
	tok := csrfFor(t, e, admin)

	resp, _ := postForm(t, e, admin, "/admin/settings/service-number", url.Values{"csrf": {tok}, "device_id": {"d_other"}})
	require.NotEqual(t, http.StatusSeeOther, resp.StatusCode)
	_, ok, _ := e.st.Setting(context.Background(), store.SettingServiceDeviceID)
	require.False(t, ok)

	resp, _ = postForm(t, e, admin, "/admin/settings/service-number", url.Values{"csrf": {tok}, "device_id": {"d_admin"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	v, _, _ := e.st.Setting(context.Background(), store.SettingServiceDeviceID)
	require.Equal(t, "d_admin", v)
}

func TestAdminSigninLinkIsShownNotSent(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, otherID := signedIn(t, e, "+27825550101")
	tok := csrfFor(t, e, admin)
	resp, body := postForm(t, e, admin, "/admin/users/"+otherID+"/signin-link", url.Values{"csrf": {tok}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "/signin/verify?t=")
	require.Contains(t, body, "Valid once, for 15 minutes")
}

func TestAdminSettingsPageShowsCallerDevicesAndStats(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_admin", PhoneNumber: "27825550100"}, {ID: "d_unlinked"}}}
	e := withDevices(t, stub)
	admin, adminID := signedIn(t, e, "+27825550100")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_admin", adminID))
	require.NoError(t, e.st.SetOwner(context.Background(), "d_unlinked", adminID))

	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	req.AddCookie(admin)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "27 82 555 0100", "the linked device is offered as a service number")
	require.NotContains(t, body, `value="d_unlinked"`, "an unlinked device slot is not offered")
	require.Contains(t, body, "Numbers linked")
}

func TestDeviceName(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36 Edg/119.0.0.0":           "Edge on Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36 Edg/119.0.0.0":     "Edge on macOS",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1": "Safari on iOS",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Mobile Safari/537.36":                   "Chrome on Android",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0":                                                        "Firefox on Windows",
		"curl/8.0.1": "This device",
		"":           "This device",
	}
	for ua, want := range cases {
		require.Equal(t, want, deviceName(ua), ua)
	}
}

// Behaviour: an account whose deletion has started cannot be re-enabled from
// the admin page; it stays deleting and the admin is told why.
func TestEnablingADeletingUserIsRefused(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, victimID := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetUserStatus(context.Background(), victimID, store.StatusDeleting))
	tok := csrfFor(t, e, admin)

	resp, body := postForm(t, e, admin, "/admin/users/"+victimID+"/status", url.Values{"csrf": {tok}, "status": {"active"}})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, body, "being deleted")
	u, _ := e.st.UserByID(context.Background(), victimID)
	require.Equal(t, store.StatusDeleting, u.Status)
}
