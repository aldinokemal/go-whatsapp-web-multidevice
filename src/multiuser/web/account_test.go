package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

func TestAccountPageShowsPhoneAndEmptyState(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, _ := signedIn(t, e, "+27825550101")
	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(ck)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "&#43;27 82 555 0101", "shown grouped")
	require.Contains(t, body, "No passkeys yet")
	require.Contains(t, body, "Add a passkey")
}

func TestAccountPageListsOwnPasskeysOnly(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	require.NoError(t, e.st.AddPasskey(context.Background(), store.Passkey{
		CredentialID: "cred_mine", UserID: me, Name: "Safari on iOS", CredentialJSON: []byte(`{}`),
	}))
	require.NoError(t, e.st.AddPasskey(context.Background(), store.Passkey{
		CredentialID: "cred_theirs", UserID: them, Name: "Chrome on Android", CredentialJSON: []byte(`{}`),
	}))

	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(ck)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "Safari on iOS")
	require.NotContains(t, body, "Chrome on Android")
	require.Contains(t, body, "1 passkey")
}

func TestDeletingAnotherUsersPasskeyIsNotFound(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	require.NoError(t, e.st.AddPasskey(context.Background(), store.Passkey{
		CredentialID: "cred_theirs", UserID: them, Name: "Chrome on Android", CredentialJSON: []byte(`{}`),
	}))
	csrf := csrfFor(t, e, ck)

	resp, _ := postForm(t, e, ck, "/account/passkeys/cred_theirs/delete", url.Values{"csrf": {csrf}})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	list, err := e.st.PasskeysOf(context.Background(), them)
	require.NoError(t, err)
	require.Len(t, list, 1, "the other user's passkey survives untouched")
	_ = me
}

func TestDeletingOwnPasskeyRedirectsToAccount(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.AddPasskey(context.Background(), store.Passkey{
		CredentialID: "cred_mine", UserID: me, Name: "Safari on iOS", CredentialJSON: []byte(`{}`),
	}))
	csrf := csrfFor(t, e, ck)

	resp, _ := postForm(t, e, ck, "/account/passkeys/cred_mine/delete", url.Values{"csrf": {csrf}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/account", resp.Header.Get("Location"))

	list, err := e.st.PasskeysOf(context.Background(), me)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestSignoutEverywhereNeedsCSRFAndEndsAllSessions(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")

	noCSRF := httptest.NewRequest(http.MethodPost, "/account/signout-everywhere", nil)
	noCSRF.AddCookie(ck)
	resp, _ := e.do(t, noCSRF)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	csrf := csrfFor(t, e, ck)
	resp, _ = postForm(t, e, ck, "/account/signout-everywhere", url.Values{"csrf": {csrf}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/signin", resp.Header.Get("Location"))

	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(ck)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "the session that just signed everyone out is itself gone")
	_ = me
}

func TestPasskeyRegisterBeginNeedsCSRFAndSetsCookie(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, _ := signedIn(t, e, "+27825550101")

	noCSRF := httptest.NewRequest(http.MethodPost, "/auth/passkey/register/begin", nil)
	noCSRF.AddCookie(ck)
	resp, _ := e.do(t, noCSRF)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	csrf := csrfFor(t, e, ck)
	resp, body := postForm(t, e, ck, "/auth/passkey/register/begin", url.Values{"csrf": {csrf}})
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	require.Contains(t, body, `"challenge"`)
	var gotCookie bool
	for _, c := range resp.Cookies() {
		gotCookie = gotCookie || (c.Name == pkRegCookie && c.Value != "")
	}
	require.True(t, gotCookie, "the ceremony cookie must be set so .../finish can find it")
}
