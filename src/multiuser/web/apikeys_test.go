package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

var rawKeyRe = regexp.MustCompile(store.APIKeyPrefix + `[A-Za-z0-9_-]{43}`)

func createKey(t *testing.T, e *env, ck *http.Cookie, name string) (*http.Response, string) {
	t.Helper()
	return postForm(t, e, ck, "/account/api-keys", url.Values{"csrf": {csrfFor(t, e, ck)}, "name": {name}})
}

// Behaviour + security: the raw key is in the create response, works, and is
// never shown again; the list shows only its name and prefix.
func TestCreateKeyShowsItOnce(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	resp, body := createKey(t, e, ck, "Laptop script")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw := rawKeyRe.FindString(body)
	require.NotEmpty(t, raw, "the new key is shown")
	require.Contains(t, body, "You won't see it again")
	require.Contains(t, body, "https://wtf.onrender.com/devices", "the usage line names this server")
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))

	u, _, err := e.st.UserByAPIKey(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, me, u.ID)

	_, page := fetchPage(t, e, ck, "/account")
	require.NotContains(t, page, raw)
	require.NotContains(t, page, raw[len(store.APIKeyPrefix)+6:], "not even the secret part")
	require.Contains(t, page, "Laptop script")
	require.Contains(t, page, raw[:len(store.APIKeyPrefix)+6], "the display prefix")
	require.Contains(t, page, "1 key")
}

func TestCreateKeyNeedsAName(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	resp, body := createKey(t, e, ck, "   ")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, `class="form-error"`)
	require.Empty(t, rawKeyRe.FindString(body))
	keys, _ := e.st.APIKeysOf(context.Background(), me)
	require.Empty(t, keys)
}

func TestCreateKeyNeedsCSRF(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	resp, _ := postForm(t, e, ck, "/account/api-keys", url.Values{"name": {"x"}})
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	keys, _ := e.st.APIKeysOf(context.Background(), me)
	require.Empty(t, keys)
}

// Shape (F2): Revoke opens a confirm dialog; nothing on the page posts to a
// revoke URL directly.
func TestRevokeAsksFirst(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	_, k, err := e.st.CreateAPIKey(context.Background(), me, "Laptop script")
	require.NoError(t, err)
	_, page := fetchPage(t, e, ck, "/account")
	require.Contains(t, page, `data-open-dialog="revoke-key"`)
	require.Contains(t, page, `data-dialog-action="/account/api-keys/`+k.ID+`/revoke"`)
	require.NotContains(t, page, ` action="/account/api-keys/`+k.ID+`/revoke"`, "no form posts there directly")
	require.Contains(t, page, `id="revoke-key"`)
}

func TestRevokeOwnKey(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	raw, k, err := e.st.CreateAPIKey(context.Background(), me, "k")
	require.NoError(t, err)
	resp, _ := postForm(t, e, ck, "/account/api-keys/"+k.ID+"/revoke", url.Values{"csrf": {csrfFor(t, e, ck)}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/account", resp.Header.Get("Location"))
	_, _, err = e.st.UserByAPIKey(context.Background(), raw)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Security: another user's key id is a 404 and their key keeps working.
func TestRevokeAnotherUsersKeyIs404(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, _ := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	raw, k, err := e.st.CreateAPIKey(context.Background(), them, "theirs")
	require.NoError(t, err)
	resp, _ := postForm(t, e, ck, "/account/api-keys/"+k.ID+"/revoke", url.Values{"csrf": {csrfFor(t, e, ck)}})
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html", "a page route answers with a page")
	_, _, err = e.st.UserByAPIKey(context.Background(), raw)
	require.NoError(t, err)

	_, page := fetchPage(t, e, ck, "/account")
	require.NotContains(t, page, "theirs")
}

// V1: at the limit the New key button is hidden and the count says so.
func TestNewKeyHiddenAtTheLimit(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	_, page := fetchPage(t, e, ck, "/account")
	require.Contains(t, page, `data-open-dialog="new-key"`)
	require.Contains(t, page, "No API keys")
	for i := 0; i < store.MaxAPIKeysPerUser; i++ {
		_, _, err := e.st.CreateAPIKey(context.Background(), me, "k")
		require.NoError(t, err)
	}
	_, page = fetchPage(t, e, ck, "/account")
	require.NotContains(t, page, `data-open-dialog="new-key"`)
	require.Contains(t, page, "10 of 10 keys")

	resp, body := createKey(t, e, ck, "one more")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, `class="form-error"`)
	require.Empty(t, rawKeyRe.FindString(body))
}

// Security: an API key never reaches the key-management pages: a bearer key
// alone is an anonymous browser, sent to sign in.
func TestKeyPagesRefuseAPIKeyAuth(t *testing.T) {
	e := withDevices(t, &devStub{})
	_, me := signedIn(t, e, "+27825550101")
	raw, k, err := e.st.CreateAPIKey(context.Background(), me, "k")
	require.NoError(t, err)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/account"},
		{http.MethodPost, "/account/api-keys"},
		{http.MethodPost, "/account/api-keys/" + k.ID + "/revoke"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("name=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Authorization", "Bearer "+raw)
		resp, _ := e.do(t, req)
		require.Equal(t, http.StatusSeeOther, resp.StatusCode, tc.path)
		require.True(t, strings.HasPrefix(resp.Header.Get("Location"), "/signin"), tc.path)
	}
	keys, _ := e.st.APIKeysOf(context.Background(), me)
	require.Len(t, keys, 1, "nothing created or revoked")
}

func TestKeyActionsAreAuditedWithoutTheKey(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, me := signedIn(t, e, "+27825550101")
	_, body := createKey(t, e, ck, "k")
	raw := rawKeyRe.FindString(body)
	keys, _ := e.st.APIKeysOf(context.Background(), me)
	_, _ = postForm(t, e, ck, "/account/api-keys/"+keys[0].ID+"/revoke", url.Values{"csrf": {csrfFor(t, e, ck)}})

	events, _, err := e.st.ListAudit(context.Background(), store.AuditQuery{UserID: me})
	require.NoError(t, err)
	var actions []string
	for _, ev := range events {
		actions = append(actions, ev.Action)
		require.NotContains(t, ev.Detail, raw[len(store.APIKeyPrefix):])
	}
	require.Contains(t, actions, "api_key_created")
	require.Contains(t, actions, "api_key_revoked")
}
