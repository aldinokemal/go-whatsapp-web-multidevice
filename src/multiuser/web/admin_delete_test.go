package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/account"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

// adminDeleteEnv is a superadmin signed in, plus one disabled and one active
// member, with Delete wired to rec.
func adminDeleteEnv(t *testing.T, rec *deleteRecorder) (e *env, ck *http.Cookie, adminID, disabledID, activeID string) {
	t.Helper()
	e = withDeleter(t, rec.Delete)
	ck, adminID = signedIn(t, e, "+27825550100")
	ctx := context.Background()
	off, err := e.st.CreateUser(ctx, "+27825550111")
	require.NoError(t, err)
	require.NoError(t, e.st.SetUserStatus(ctx, off.ID, store.StatusDisabled))
	require.NoError(t, e.st.SetOwner(ctx, "d_a", off.ID))
	on, err := e.st.CreateUser(ctx, "+27825550122")
	require.NoError(t, err)
	return e, ck, adminID, off.ID, on.ID
}

func postAdminDelete(t *testing.T, e *env, ck *http.Cookie, id, csrf, typed string) (*http.Response, string) {
	t.Helper()
	form := url.Values{"csrf": {csrf}, "confirm": {typed}}
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+id+"/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	if ck != nil {
		req.AddCookie(ck)
	}
	return e.do(t, req)
}

// rowOf is one user's row on Users, up to the next row.
func rowOf(body, id string) string {
	return regexp.MustCompile(`(?s)<div class="trow[^"]*" role="row" id="user-` + regexp.QuoteMeta(id) + `".*?\n    </div>`).FindString(body)
}

// Behaviour: a disabled user's ⋯ menu ends with a red Delete account; an
// active user's, and the admin's own, never offer it. Its dialog says what
// goes and asks for their number.
func TestAdminCanDeleteADisabledUser(t *testing.T) {
	rec := &deleteRecorder{}
	e, ck, adminID, offID, onID := adminDeleteEnv(t, rec)
	body := get(t, e, ck, "/admin/users")

	off := rowOf(body, offID)
	require.NotEmpty(t, off)
	items := regexp.MustCompile(`(?s)class="menu-item[^"]*"`).FindAllString(off, -1)
	require.NotEmpty(t, items)
	require.Contains(t, items[len(items)-1], "menu-item-danger", "Delete account is last, in red")
	require.Contains(t, off, `data-open-dialog="delete-`+offID+`"`)
	require.Contains(t, off, "Delete account")
	require.NotContains(t, rowOf(body, onID), "Delete account", "an active user is disabled first")
	require.NotContains(t, rowOf(body, adminID), "Delete account", "never on your own row")

	dialog := regexp.MustCompile(`(?s)<dialog id="delete-` + offID + `".*?</dialog>`).FindString(body)
	require.NotEmpty(t, dialog)
	require.Contains(t, dialog, "&#43;27 82 555 0111")
	require.Contains(t, dialog, "1 linked WhatsApp number", "says what goes, with the real count")
	require.Contains(t, dialog, "API keys")
	require.Contains(t, dialog, "Type their number to confirm")
	require.NotContains(t, dialog, "DELETE", "the admin types the number itself")

	csrf := csrfFor(t, e, ck)
	for _, typed := range []string{"", "DELETE", "+27825550122", "0111"} {
		resp, frag := postAdminDelete(t, e, ck, offID, csrf, typed)
		require.Equal(t, http.StatusOK, resp.StatusCode, typed)
		require.Empty(t, resp.Header.Get("HX-Redirect"), typed)
		require.Contains(t, frag, "Type their number to confirm.", typed)
		require.Contains(t, frag, `aria-invalid="true"`, typed)
	}
	require.Empty(t, rec.calls)

	resp, _ := postAdminDelete(t, e, ck, offID, csrf, "+27 82 555 0111")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "/admin/users", resp.Header.Get("HX-Redirect"))
	require.Equal(t, []string{offID}, rec.calls)

	ev, _, err := e.st.ListAudit(context.Background(), store.AuditQuery{UserID: offID, Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, ev)
	require.Equal(t, "user_deleted", ev[0].Action)
	require.Equal(t, adminID, ev[0].ActorID, "audited with the admin as actor")
}

// Security: only a disabled account, never your own, never the owner of the
// sign-in number; superadmin only and CSRF-checked.
func TestAdminDeleteIsRefusedWhereItMustBe(t *testing.T) {
	rec := &deleteRecorder{}
	e, ck, adminID, offID, onID := adminDeleteEnv(t, rec)
	csrf := csrfFor(t, e, ck)

	_, frag := postAdminDelete(t, e, ck, onID, csrf, "+27825550122")
	require.Contains(t, frag, "Disable this account first.")
	_, frag = postAdminDelete(t, e, ck, adminID, csrf, "+27825550100")
	require.Contains(t, frag, "delete your own account here.")
	resp, _ := postAdminDelete(t, e, ck, "no-such-user", csrf, "+27825550111")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	// CSRF.
	resp, _ = postAdminDelete(t, e, ck, offID, "wrong", "+27825550111")
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	// A member: 404, as every admin route.
	member, _ := signedIn(t, e, "+27825550133")
	resp, _ = postAdminDelete(t, e, member, offID, csrfFor(t, e, member), "+27825550111")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	// Signed out: off to sign in.
	resp, _ = postAdminDelete(t, e, nil, offID, "", "+27825550111")
	require.NotEqual(t, http.StatusOK, resp.StatusCode)
	require.Empty(t, rec.calls)

	// The sign-in number's owner: refused, with where to go.
	ctx := context.Background()
	require.NoError(t, e.st.SetOwner(ctx, "d_svc", offID))
	require.NoError(t, e.st.SetSetting(ctx, store.SettingServiceDeviceID, "d_svc", adminID))
	body := get(t, e, ck, "/admin/users")
	require.NotContains(t, rowOf(body, offID), "Delete account", "no control the server would refuse")
	_, frag = postAdminDelete(t, e, ck, offID, csrf, "+27825550111")
	require.Contains(t, frag, "has the sign-in number")
	require.Contains(t, frag, `href="/admin/settings"`)
	require.Empty(t, rec.calls)
}

// Behaviour: the real deleter is what runs, and the account is gone.
func TestAdminDeleteRemovesTheAccount(t *testing.T) {
	var e *env
	e = withDeleter(t, func(ctx context.Context, id string) error {
		return (&account.Deleter{Store: e.st, Devices: &devStub{}}).Delete(ctx, id)
	})
	ck, _ := signedIn(t, e, "+27825550100")
	ctx := context.Background()
	off, err := e.st.CreateUser(ctx, "+27825550111")
	require.NoError(t, err)
	require.NoError(t, e.st.SetUserStatus(ctx, off.ID, store.StatusDisabled))
	resp, _ := postAdminDelete(t, e, ck, off.ID, csrfFor(t, e, ck), "27825550111")
	require.Equal(t, "/admin/users", resp.Header.Get("HX-Redirect"))
	_, err = e.st.UserByID(ctx, off.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}
