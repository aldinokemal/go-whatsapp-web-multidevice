package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/account"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

// deleteRecorder stands in for account.Deleter.Delete.
type deleteRecorder struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (r *deleteRecorder) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, id)
	return r.err
}

func withDeleter(t *testing.T, del func(context.Context, string) error) *env {
	t.Helper()
	e := newEnv(t)
	e.d.DeleteAccount = del
	remount(e, &devStub{devices: []domainDevice.Device{{ID: "d_a"}, {ID: "d_svc"}}})
	return e
}

func postDelete(t *testing.T, e *env, ck *http.Cookie, csrf, typed string, htmx bool) (*http.Response, string) {
	t.Helper()
	form := url.Values{"csrf": {csrf}, "confirm": {typed}}
	req := httptest.NewRequest(http.MethodPost, "/account/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	if ck != nil {
		req.AddCookie(ck)
	}
	return e.do(t, req)
}

// Behaviour: Account ends with a Delete account card whose dialog says what
// goes, and asks for the number or DELETE.
func TestAccountOffersDeleteAccount(t *testing.T) {
	rec := &deleteRecorder{}
	e := withDeleter(t, rec.Delete)
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_a", me))
	body := get(t, e, ck, "/account")
	require.Contains(t, body, `data-open-dialog="delete-account"`)
	dialog := regexp.MustCompile(`(?s)<dialog id="delete-account".*?</dialog>`).FindString(body)
	require.NotEmpty(t, dialog)
	require.Contains(t, dialog, `name="confirm"`)
	require.Contains(t, dialog, "DELETE")
	require.Contains(t, dialog, "1 linked WhatsApp number", "says what is removed, with the real count")
	require.Contains(t, dialog, "API keys")
	require.Less(t, strings.Index(body, "sessions-title"), strings.Index(body, "delete-account-title"), "the danger zone is last")
}

// Behaviour: anything but the person's number or DELETE is refused in the
// dialog and nothing is deleted.
func TestDeleteNeedsTheNumberOrDELETE(t *testing.T) {
	rec := &deleteRecorder{}
	e := withDeleter(t, rec.Delete)
	ck, _ := signedIn(t, e, "+27825550101")
	csrf := csrfFor(t, e, ck)
	for _, typed := range []string{"", "yes", "0101", "+27825550102", "DELET"} {
		resp, body := postDelete(t, e, ck, csrf, typed, true)
		require.Equal(t, http.StatusOK, resp.StatusCode, typed)
		require.Contains(t, body, "Type your number, or DELETE, to confirm.", typed)
		require.Contains(t, body, `aria-invalid="true"`)
	}
	require.Empty(t, rec.calls)
}

// Behaviour: the number in any spelling, or DELETE in any case, deletes;
// the person is signed out and lands on sign-in with a notice.
func TestDeleteSignsOutAndLandsOnSignin(t *testing.T) {
	for _, typed := range []string{"+27 82 555 0101", "27825550101", "delete", " DELETE "} {
		rec := &deleteRecorder{}
		e := withDeleter(t, rec.Delete)
		ck, me := signedIn(t, e, "+27825550101")
		csrf := csrfFor(t, e, ck)
		resp, _ := postDelete(t, e, ck, csrf, typed, true)
		require.Equal(t, http.StatusOK, resp.StatusCode, typed)
		require.Equal(t, "/signin?deleted=1", resp.Header.Get("HX-Redirect"))
		require.Equal(t, []string{me}, rec.calls)
		cleared := false
		for _, c := range resp.Cookies() {
			cleared = cleared || (c.Value == "" && c.MaxAge < 0)
		}
		require.True(t, cleared, "the session cookie is cleared")
	}
	// Without htmx: a plain redirect.
	rec := &deleteRecorder{}
	e := withDeleter(t, rec.Delete)
	ck, _ := signedIn(t, e, "+27825550101")
	resp, _ := postDelete(t, e, ck, csrfFor(t, e, ck), "DELETE", false)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/signin?deleted=1", resp.Header.Get("Location"))
}

// Behaviour: the real deleter removes the account; the old cookie no longer
// signs anyone in.
func TestDeleteRemovesTheAccount(t *testing.T) {
	var e *env
	e = withDeleter(t, func(ctx context.Context, id string) error {
		return (&account.Deleter{Store: e.st, Devices: &devStub{}}).Delete(ctx, id)
	})
	ck, me := signedIn(t, e, "+27825550101")
	resp, _ := postDelete(t, e, ck, csrfFor(t, e, ck), "DELETE", true)
	require.Equal(t, "/signin?deleted=1", resp.Header.Get("HX-Redirect"))
	_, err := e.st.UserByID(context.Background(), me)
	require.ErrorIs(t, err, store.ErrNotFound)
	resp, _ = e.do(t, func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/account", nil)
		r.AddCookie(ck)
		return r
	}())
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
}

// Behaviour: the owner of the sign-in number is told where to go, in the
// dialog, and the card itself says so instead of offering the button.
func TestServiceNumberOwnerSeesWhereToGo(t *testing.T) {
	rec := &deleteRecorder{err: account.ErrOwnsServiceNumber}
	e := withDeleter(t, rec.Delete)
	ck, me := signedIn(t, e, "+27825550100") // a superadmin
	require.NoError(t, e.st.SetOwner(context.Background(), "d_svc", me))
	require.NoError(t, e.st.SetSetting(context.Background(), store.SettingServiceDeviceID, "d_svc", me))

	body := get(t, e, ck, "/account")
	require.NotContains(t, body, `data-open-dialog="delete-account"`, "no button the server would refuse")
	require.Contains(t, body, "has the sign-in number")
	require.Contains(t, body, `href="/admin/settings"`)

	resp, frag := postDelete(t, e, ck, csrfFor(t, e, ck), "DELETE", true)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, frag, "has the sign-in number")
	require.Contains(t, frag, `href="/admin/settings"`)
	require.Empty(t, resp.Header.Get("HX-Redirect"))
}

// Behaviour: a deletion that stopped half way still signs out (the account
// is already refused everywhere and finishes at start-up); one that never
// started says so.
func TestDeleteFailureIsHonest(t *testing.T) {
	var e *env
	e = withDeleter(t, func(ctx context.Context, id string) error {
		_ = e.st.SetUserStatus(ctx, id, store.StatusDeleting)
		return errors.New("whatsapp said no")
	})
	ck, _ := signedIn(t, e, "+27825550101")
	resp, _ := postDelete(t, e, ck, csrfFor(t, e, ck), "DELETE", true)
	require.Equal(t, "/signin?deleted=1", resp.Header.Get("HX-Redirect"))

	e2 := withDeleter(t, func(context.Context, string) error { return errors.New("disk full") })
	ck2, _ := signedIn(t, e2, "+27825550101")
	resp, frag := postDelete(t, e2, ck2, csrfFor(t, e2, ck2), "DELETE", true)
	require.Empty(t, resp.Header.Get("HX-Redirect"))
	require.Contains(t, frag, "delete your account. Try again.")
	require.NotContains(t, frag, `aria-invalid`, "the typing was fine")
}

// Security: CSRF is required, and an API key cannot delete an account.
func TestDeleteNeedsCSRFAndASession(t *testing.T) {
	rec := &deleteRecorder{}
	e := withDeleter(t, rec.Delete)
	ck, me := signedIn(t, e, "+27825550101")
	resp, _ := postDelete(t, e, ck, "wrong", "DELETE", false)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	key, _, err := e.st.CreateAPIKey(context.Background(), me, "script")
	require.NoError(t, err)
	form := url.Values{"confirm": {"DELETE"}}
	req := httptest.NewRequest(http.MethodPost, "/account/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "a key is not a session: off to sign in")
	require.Empty(t, rec.calls)
}

// Behaviour: sign-in says the account was deleted.
func TestSigninSaysTheAccountWasDeleted(t *testing.T) {
	e := newEnv(t)
	body := get(t, e, nil, "/signin?deleted=1")
	require.Contains(t, body, "Your account was deleted.")
	require.NotContains(t, get(t, e, nil, "/signin"), "Your account was deleted.")
}

// Shape: a danger button outside a dialog only ever opens a confirmation; it
// never submits anything itself.
func TestDangerButtonsOutsideDialogsOnlyOpenOne(t *testing.T) {
	entries, err := templatesFS.ReadDir("templates")
	require.NoError(t, err)
	dialogRE := regexp.MustCompile(`(?s)<dialog.*?</dialog>`)
	btnRE := regexp.MustCompile(`<button[^>]*\bbtn-danger\b[^>]*>`)
	for _, f := range entries {
		b, _ := templatesFS.ReadFile("templates/" + f.Name())
		outside := dialogRE.ReplaceAllString(string(b), "")
		// The confirm form partial is drawn inside a dialog by "confirm".
		outside = regexp.MustCompile(`(?s)\{\{define "confirm_form"\}\}.*?(\{\{define |\z)`).ReplaceAllString(outside, "$1")
		for _, m := range btnRE.FindAllString(outside, -1) {
			require.Contains(t, m, `type="button"`, "%s: %s", f.Name(), m)
			require.Contains(t, m, "data-open-dialog", "%s: %s", f.Name(), m)
		}
	}
}
