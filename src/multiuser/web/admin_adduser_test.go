package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func postAddUser(t *testing.T, e *env, ck *http.Cookie, csrf, phone string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(url.Values{"phone": {phone}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	req.AddCookie(ck)
	return e.do(t, req)
}

func TestAddUserIsSuperadminOnly(t *testing.T) {
	e := withDevices(t, &devStub{})
	plain, _ := signedIn(t, e, "+27825550101")
	resp, _ := postAddUser(t, e, plain, csrfFor(t, e, plain), "+15555550123")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	_, err := e.st.UserByPhone(context.Background(), "+15555550123")
	require.Error(t, err, "no account created")
}

func TestAddUserNeedsCSRF(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	resp, _ := postAddUser(t, e, admin, "", "+15555550123")
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	_, err := e.st.UserByPhone(context.Background(), "+15555550123")
	require.Error(t, err, "no account created")
}

func TestAddUserRejectsABadPhoneInTheModal(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	resp, body := postAddUser(t, e, admin, csrfFor(t, e, admin), "12ab")
	require.Equal(t, http.StatusOK, resp.StatusCode, "htmx swaps 200s only, so the error stays in the modal")
	require.Contains(t, body, "Enter a full number with its country code")
	require.Contains(t, body, `value="12ab"`, "what was typed is kept")
	require.NotContains(t, body, "/signin/verify?t=")
}

func TestAddUserRefusesAnExistingNumber(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, _ = signedIn(t, e, "+27825550101")
	resp, body := postAddUser(t, e, admin, csrfFor(t, e, admin), "+27 82 555 0101")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "already has an account")
	require.NotContains(t, body, "/signin/verify?t=")
}

func TestAddUserCreatesTheAccountAndShowsItsSignInLink(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	resp, body := postAddUser(t, e, admin, csrfFor(t, e, admin), "+1 (555) 555-0123")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "https://wtf.onrender.com/signin/verify?t=")
	require.Contains(t, body, "data-copy")

	u, err := e.st.UserByPhone(context.Background(), "+15555550123")
	require.NoError(t, err, "stored normalised")
	require.Equal(t, "active", u.Status)

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.AddCookie(admin)
	_, list := e.do(t, req)
	require.Contains(t, list, "Just added", "the new row is marked")
	require.Less(t, strings.Index(list, "Just added"), strings.LastIndex(list, "+27 82 555 0100"), "and listed first")
}
