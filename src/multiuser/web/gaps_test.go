package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func fetchPage(t *testing.T, e *env, ck *http.Cookie, path string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ck != nil {
		req.AddCookie(ck)
	}
	return e.do(t, req)
}

// ---- Gap A and B: the row says what happened to a number ----

func TestARowSignedOutFromThePhoneKeepsItsNumber(t *testing.T) {
	e := withDevices(t, &devStub{devices: []domainDevice.Device{{ID: "d_out", State: domainDevice.DeviceStateDisconnected}}})
	ck, me := signedIn(t, e, "+27825550101")
	ctx := context.Background()
	require.NoError(t, e.st.SetOwner(ctx, "d_out", me))
	require.NoError(t, e.st.MarkSignedOut(ctx, "d_out", "27825550177", "Shop phone"))

	_, body := fetchPage(t, e, ck, "/")
	require.Contains(t, body, "Shop phone")
	require.Contains(t, body, "Signed out from your phone. Link it again.")
	require.NotContains(t, body, "Not linked yet")
}

func TestARefusedDuplicateSaysWhyUntilTheNextGoodLink(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_dup"}}}
	e := withDevices(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	ctx := context.Background()
	require.NoError(t, e.st.SetOwner(ctx, "d_dup", me))
	require.NoError(t, e.st.RecordRefusal(ctx, "d_dup", store.RefusalDuplicate))

	_, body := fetchPage(t, e, ck, "/")
	require.Contains(t, body, "That number is already linked to another account.")

	require.NoError(t, e.st.ClearRefusal(ctx, "d_dup"))
	stub.devices = []domainDevice.Device{{ID: "d_dup", PhoneNumber: "27825550188", State: domainDevice.DeviceStateLoggedIn}}
	_, body = fetchPage(t, e, ck, "/")
	require.NotContains(t, body, "already linked to another account")
	require.Contains(t, body, "Connected")
}

// ---- Gap C: superadmin user management ----

func TestAdminSeesAUsersNumbersAndCanUnlinkOne(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{
		{ID: "d_x", PhoneNumber: "27825550177", State: domainDevice.DeviceStateLoggedIn},
		{ID: "d_mine", PhoneNumber: "27825550100", State: domainDevice.DeviceStateLoggedIn},
	}}
	e := withDevices(t, stub)
	admin, adminID := signedIn(t, e, "+27825550100")
	_, userID := signedIn(t, e, "+27825550101")
	ctx := context.Background()
	require.NoError(t, e.st.SetOwner(ctx, "d_x", userID))
	require.NoError(t, e.st.SetOwner(ctx, "d_mine", adminID))

	_, body := fetchPage(t, e, admin, "/admin/users")
	require.Contains(t, body, `aria-expanded="false"`, "the numbers sit behind a chevron")
	require.Contains(t, body, "/admin/users/"+userID+"/devices/d_x/unlink")
	require.NotContains(t, body, "/admin/users/"+userID+"/devices/d_mine/unlink", "only that user's own numbers")

	tok := csrfFor(t, e, admin)
	// A number that isn't this user's is not found, and stays.
	resp, _ := postForm(t, e, admin, "/admin/users/"+userID+"/devices/d_mine/unlink", url.Values{"csrf": {tok}})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	_, err := e.st.OwnerOf(ctx, "d_mine")
	require.NoError(t, err)

	resp, _ = postForm(t, e, admin, "/admin/users/"+userID+"/devices/d_x/unlink", url.Values{"csrf": {tok}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	_, err = e.st.OwnerOf(ctx, "d_x")
	require.ErrorIs(t, err, store.ErrNotFound)
	rows, _, _ := e.st.ListAudit(ctx, store.AuditQuery{UserID: userID})
	require.Equal(t, "device_remove", rows[0].Action)
	require.Equal(t, adminID, rows[0].ActorID)
}

func TestAdminActionsAreRefusedToOthersAndNeedCSRF(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	plain, plainID := signedIn(t, e, "+27825550101")
	_, otherID := signedIn(t, e, "+27825550102")
	ptok := csrfFor(t, e, plain)
	for _, path := range []string{
		"/admin/users/" + otherID + "/limit", "/admin/users/" + otherID + "/phone",
		"/admin/users/" + otherID + "/devices/d_x/unlink",
	} {
		resp, _ := postForm(t, e, plain, path, url.Values{"csrf": {ptok}, "max_devices": {"9"}, "phone": {"+27825550999"}})
		require.Equal(t, http.StatusNotFound, resp.StatusCode, path)
		resp, _ = postForm(t, e, admin, path, url.Values{"max_devices": {"9"}, "phone": {"+27825550999"}})
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "no CSRF token: %s", path)
	}
	resp, _ := fetchPage(t, e, plain, "/admin/activity")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	_ = plainID
}

func TestDisableCanAlsoUnlinkTheirNumbers(t *testing.T) {
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_1"}, {ID: "d_2"}, {ID: "d_3"}}}
	e := withDevices(t, stub)
	admin, _ := signedIn(t, e, "+27825550100")
	_, keepID := signedIn(t, e, "+27825550101")
	_, dropID := signedIn(t, e, "+27825550102")
	ctx := context.Background()
	require.NoError(t, e.st.SetOwner(ctx, "d_1", keepID))
	require.NoError(t, e.st.SetOwner(ctx, "d_2", dropID))
	require.NoError(t, e.st.SetOwner(ctx, "d_3", dropID))
	tok := csrfFor(t, e, admin)

	_, body := fetchPage(t, e, admin, "/admin/users")
	require.Contains(t, body, `name="unlink"`)
	require.Regexp(t, regexp.MustCompile(`name="unlink"[^>]*checked`), body, "on by default")

	resp, _ := postForm(t, e, admin, "/admin/users/"+keepID+"/status", url.Values{"csrf": {tok}, "status": {"disabled"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	ids, _ := e.st.DevicesOf(ctx, keepID)
	require.Len(t, ids, 1, "unticked: their numbers stay")

	resp, _ = postForm(t, e, admin, "/admin/users/"+dropID+"/status", url.Values{"csrf": {tok}, "status": {"disabled"}, "unlink": {"on"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	ids, _ = e.st.DevicesOf(ctx, dropID)
	require.Empty(t, ids)
}

func TestAdminSetsANumberLimitWithinRange(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, userID := signedIn(t, e, "+27825550101")
	tok := csrfFor(t, e, admin)

	for _, bad := range []string{"21", "-1", "two", ""} {
		resp, body := postForm(t, e, admin, "/admin/users/"+userID+"/limit", url.Values{"csrf": {tok}, "max_devices": {bad}})
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, bad)
		require.Contains(t, body, "Choose a number from 0 to 20.")
	}
	resp, _ := postForm(t, e, admin, "/admin/users/"+userID+"/limit", url.Values{"csrf": {tok}, "max_devices": {"5"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	u, _ := e.st.UserByID(context.Background(), userID)
	require.Equal(t, 5, u.MaxDevices)
	rows, _, _ := e.st.ListAudit(context.Background(), store.AuditQuery{UserID: userID})
	require.Equal(t, "user_limit_set", rows[0].Action)
	require.Equal(t, "5", rows[0].Detail)
}

func TestAdminChangesAUsersPhone(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, adminID := signedIn(t, e, "+27825550100")
	userCk, userID := signedIn(t, e, "+27825550101")
	_, _ = signedIn(t, e, "+27825550102")
	tok := csrfFor(t, e, admin)
	ctx := context.Background()

	resp, body := postForm(t, e, admin, "/admin/users/"+userID+"/phone", url.Values{"csrf": {tok}, "phone": {"+27 82 555 0102"}})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, body, "That number already belongs to another user.")

	resp, body = postForm(t, e, admin, "/admin/users/"+userID+"/phone", url.Values{"csrf": {tok}, "phone": {"12"}})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, body, "country code")

	resp, _ = postForm(t, e, admin, "/admin/users/"+userID+"/phone", url.Values{"csrf": {tok}, "phone": {"+27 82 555 0100"}})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "an admin's number is never handed to someone else")

	resp, _ = postForm(t, e, admin, "/admin/users/"+adminID+"/phone", url.Values{"csrf": {tok}, "phone": {"+27825550199"}})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "not your own")

	resp, _ = postForm(t, e, admin, "/admin/users/"+userID+"/phone", url.Values{"csrf": {tok}, "phone": {"(+27) 82-555-0199"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	u, _ := e.st.UserByID(ctx, userID)
	require.Equal(t, "+27825550199", u.Phone)

	resp, _ = fetchPage(t, e, userCk, "/")
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "their sessions ended")

	rows, _, _ := e.st.ListAudit(ctx, store.AuditQuery{UserID: userID})
	require.Equal(t, "user_phone_changed", rows[0].Action)
	require.Equal(t, adminID, rows[0].ActorID)
}

// ---- Gap D: sessions on Account ----

var rawSessionLike = regexp.MustCompile(`[A-Za-z0-9_-]{43}`)

func TestAccountListsMySessionsWithoutExposingIDs(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	ctx := context.Background()
	other, err := e.st.CreateSession(ctx, me, "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36", time.Hour)
	require.NoError(t, err)

	_, body := fetchPage(t, e, ck, "/account")
	require.Contains(t, body, "Chrome on macOS")
	require.Contains(t, body, "This device")
	require.NotContains(t, body, ck.Value, "the raw cookie never reaches HTML")
	require.NotContains(t, body, other)
	require.NotContains(t, body, store.HashToken(ck.Value), "nor its stored hash")
	require.NotContains(t, body, store.HashToken(other))
	require.Contains(t, body, "/account/sessions/")
}

func TestSigningOutOneSessionIsOwnerChecked(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	bobCk, bobID := signedIn(t, e, "+27825550102")
	ctx := context.Background()
	mine2, _ := e.st.CreateSession(ctx, me, "UA", time.Hour)
	bobs := e.d.Auth.SessionHandle(store.HashToken(bobCk.Value))
	tok := csrfFor(t, e, ck)

	// Bob's session cannot be named by me.
	resp, _ := postForm(t, e, ck, "/account/sessions/"+bobs+"/signout", url.Values{"csrf": {tok}})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	list, _ := e.st.SessionsOf(ctx, bobID)
	require.Len(t, list, 1)

	h := e.d.Auth.SessionHandle(store.HashToken(mine2))
	resp, _ = postForm(t, e, ck, "/account/sessions/"+h+"/signout", url.Values{})
	require.Equal(t, http.StatusForbidden, resp.StatusCode, "needs CSRF")

	resp, _ = postForm(t, e, ck, "/account/sessions/"+h+"/signout", url.Values{"csrf": {tok}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/account", resp.Header.Get("Location"))
	_, err := e.st.SessionUser(ctx, mine2, time.Hour)
	require.ErrorIs(t, err, store.ErrNotFound)

	// Signing out this very session signs me out here.
	cur := e.d.Auth.SessionHandle(store.HashToken(ck.Value))
	resp, _ = postForm(t, e, ck, "/account/sessions/"+cur+"/signout", url.Values{"csrf": {tok}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/signin", resp.Header.Get("Location"))
	resp, _ = fetchPage(t, e, ck, "/account")
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
}

// ---- Gap E: Activity ----

func TestActivityPagesAuditInPlainWords(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, adminID := signedIn(t, e, "+27825550100")
	_, userID := signedIn(t, e, "+27825550101")
	ctx := context.Background()
	for i := 0; i < 55; i++ {
		require.NoError(t, e.st.Audit(ctx, store.AuditEvent{UserID: adminID, Action: "passkey_add"}))
	}
	require.NoError(t, e.st.Audit(ctx, store.AuditEvent{UserID: userID, ActorID: adminID, Action: "user_disabled"}))

	resp, body := fetchPage(t, e, admin, "/admin/activity")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "58 events", "55 + the disable + two sign-ins")
	require.Contains(t, body, "Admin")
	require.Contains(t, body, "disabled")
	require.Contains(t, body, `href="/admin/users#user-`+userID+`"`, "people link to their row")
	require.Contains(t, body, "page=2")
	require.Equal(t, 50, strings.Count(body, `class="row activity-row"`))

	_, body = fetchPage(t, e, admin, "/admin/activity?page=2")
	require.Equal(t, 8, strings.Count(body, `class="row activity-row"`))

	_, body = fetchPage(t, e, admin, "/admin/activity?user="+userID)
	require.Contains(t, body, "2 events", "their sign-in and being disabled")
	resp, _ = fetchPage(t, e, admin, "/admin/activity?user=u_nobody")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ---- Gap F: per-number webhook ----

type webhookStub struct {
	devStub
	cfg *chatstorage.DeviceWebhookConfig
}

func (s *webhookStub) SetDeviceWebhookConfig(_ context.Context, _ string, c *chatstorage.DeviceWebhookConfig) error {
	s.cfg = c
	return nil
}
func (s *webhookStub) GetDeviceWebhookConfig(context.Context, string) (*chatstorage.DeviceWebhookConfig, error) {
	return s.cfg, nil
}

func TestWebhookDialogSavesAPublicURLAndNeverShowsTheSecret(t *testing.T) {
	stub := &webhookStub{devStub: devStub{devices: []domainDevice.Device{{ID: "d_w", PhoneNumber: "27825550177", State: domainDevice.DeviceStateLoggedIn}}}}
	e := withDevicesUsecase(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	ctx := context.Background()
	require.NoError(t, e.st.SetOwner(ctx, "d_w", me))
	require.NoError(t, e.st.SetOwner(ctx, "d_them", them))
	tok := csrfFor(t, e, ck)

	resp, _ := fetchPage(t, e, ck, "/ui/devices/d_them/webhook")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp, _ = postForm(t, e, ck, "/ui/devices/d_them/webhook", url.Values{"csrf": {tok}, "url": {"https://93.184.216.34/in"}})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp, body := postForm(t, e, ck, "/ui/devices/d_w/webhook", url.Values{"csrf": {tok}, "url": {"https://127.0.0.1/in"}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "public https://")
	require.Nil(t, stub.cfg)

	resp, body = postForm(t, e, ck, "/ui/devices/d_w/webhook", url.Values{"csrf": {tok}, "url": {"https://93.184.216.34/in"}, "secret": {"s3cr3t-value"}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "Saved.")
	require.Equal(t, "https://93.184.216.34/in", *stub.cfg.WebhookURL)
	require.Equal(t, "s3cr3t-value", stub.cfg.WebhookSecret)
	require.NotContains(t, body, "s3cr3t-value")

	// A blank secret keeps the one set before.
	_, _ = postForm(t, e, ck, "/ui/devices/d_w/webhook", url.Values{"csrf": {tok}, "url": {"https://93.184.216.35/in"}})
	require.Equal(t, "s3cr3t-value", stub.cfg.WebhookSecret)

	_, body = fetchPage(t, e, ck, "/ui/devices/d_w/webhook")
	require.Contains(t, body, "https://93.184.216.35/in")
	require.NotContains(t, body, "s3cr3t-value")

	_, _ = postForm(t, e, ck, "/ui/devices/d_w/webhook", url.Values{"csrf": {tok}, "remove": {"1"}})
	require.Equal(t, "", *stub.cfg.WebhookURL)
	require.Equal(t, "", stub.cfg.WebhookSecret)
}

func withDevicesUsecase(t *testing.T, inner domainDevice.IDeviceUsecase) *env {
	t.Helper()
	e := newEnv(t)
	e.d.Devices = owners.NewScopedDevices(inner, e.st)
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	app.Use(e.d.Auth.Middleware())
	Mount(app, e.d)
	app.Use(e.d.Auth.RequireAPI())
	e.app = app
	return e
}
