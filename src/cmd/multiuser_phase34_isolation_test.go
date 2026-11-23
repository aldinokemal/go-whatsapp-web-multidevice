package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/websocket"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// drainHub stands in for the websocket hub restServer starts: the upstream
// RemoveDevice announces each removal on an unbuffered channel.
var drainHub sync.Once

// Security (phases 3 and 4): every admin page and action, the waitlist's
// included, is a 404 to a member with a session and never answers a
// member's API key (page routes take sessions only: off to sign-in).
func TestEveryAdminRouteRefusesMembers(t *testing.T) {
	app, rt, _ := newMultiUserApp(t, uimcp.Deps{})
	ctx := context.Background()
	alice, _ := rt.Store.CreateUser(ctx, "+27825550101")
	_, _, err := rt.Store.JoinWaitlist(ctx, "+27825550111")
	require.NoError(t, err)
	raw, _ := rt.Store.CreateSession(ctx, alice.ID, "", time.Hour)
	cookie := &http.Cookie{Name: rt.Auth.CookieName(), Value: raw}
	csrf := csrfForRaw(rt, raw)
	key, _, err := rt.Store.CreateAPIKey(ctx, alice.ID, "script")
	require.NoError(t, err)

	var checked, waitlist int
	deleteWalked := false
	for _, r := range app.GetRoutes(true) {
		if !strings.HasPrefix(r.Path, "/admin") || (r.Method != fiber.MethodGet && r.Method != fiber.MethodPost) {
			continue
		}
		path := strings.ReplaceAll(fill(r.Path, "d_bob"), "/x/", "/27825550111/")
		if strings.HasSuffix(path, "/x") {
			path = strings.TrimSuffix(path, "/x") + "/27825550111"
		}
		req := httptest.NewRequest(r.Method, path, strings.NewReader(url.Values{"csrf": {csrf}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, "member session -> %s %s", r.Method, path)

		req = httptest.NewRequest(r.Method, path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		resp, err = app.Test(req)
		require.NoError(t, err)
		require.Contains(t, []int{http.StatusSeeOther, http.StatusNotFound, http.StatusUnauthorized}, resp.StatusCode, "member key -> %s %s", r.Method, path)
		checked++
		if strings.HasPrefix(r.Path, "/admin/waitlist") {
			waitlist++
		}
		deleteWalked = deleteWalked || r.Path == "/admin/users/:id/delete"
	}
	require.GreaterOrEqual(t, waitlist, 5, "the waitlist page and its four actions are in the walk")
	require.Greater(t, checked, 10)
	require.True(t, deleteWalked, "an admin's Delete account is in the walk")
	_, err = rt.Store.UserByPhone(ctx, "+27825550111")
	require.ErrorIs(t, err, store.ErrNotFound, "nobody was let in")
}

// Security (phase 3): Alice deleting her account never touches Bob's
// number, and her name for a number can't land on his.
func TestDeletingMyAccountLeavesOthersAlone(t *testing.T) {
	drainHub.Do(func() {
		go func() {
			for range websocket.Broadcast {
			}
		}()
	})
	app, rt, dm := newMultiUserApp(t, uimcp.Deps{})
	ctx := context.Background()
	alice, _ := rt.Store.CreateUser(ctx, "+27825550101")
	bob, _ := rt.Store.CreateUser(ctx, "+27825550102")
	require.NoError(t, rt.Store.SetOwner(ctx, "d_alice", alice.ID))
	require.NoError(t, rt.Store.SetOwner(ctx, "d_bob", bob.ID))
	raw, _ := rt.Store.CreateSession(ctx, alice.ID, "", time.Hour)
	cookie := &http.Cookie{Name: rt.Auth.CookieName(), Value: raw}
	csrf := csrfForRaw(rt, raw)

	post := func(path string, form url.Values) *http.Response {
		form.Set("csrf", csrf)
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp
	}
	require.Equal(t, http.StatusNotFound, post("/ui/devices/d_bob/label", url.Values{"label": {"mine"}}).StatusCode)
	resp := post("/account/delete", url.Values{"confirm": {"DELETE"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/signin?deleted=1", resp.Header.Get("Location"))

	_, err := rt.Store.UserByID(ctx, alice.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, ok := dm.GetDevice("d_alice")
	require.False(t, ok, "her number is purged")
	_, ok = dm.GetDevice("d_bob")
	require.True(t, ok, "his is untouched")
	owner, err := rt.Store.OwnerOf(ctx, "d_bob")
	require.NoError(t, err)
	require.Equal(t, bob.ID, owner)
	labels, _ := rt.Store.LabelsOf(ctx, bob.ID)
	require.Equal(t, "", labels["d_bob"])
}

// Security: only a superadmin deletes another account, only a disabled one,
// and deleting it touches nobody else's number. A member who knows a
// disabled user's id and number gets a 404 and changes nothing.
func TestOnlyASuperadminDeletesAnotherAccount(t *testing.T) {
	drainHub.Do(func() {
		go func() {
			for range websocket.Broadcast {
			}
		}()
	})
	app, rt, dm := newMultiUserAppWith(t, uimcp.Deps{}, func(s *multiuser.Settings) {
		s.Superadmins = map[string]bool{"+27825550100": true}
	})
	ctx := context.Background()
	admin, _ := rt.Store.CreateUser(ctx, "+27825550100")
	alice, _ := rt.Store.CreateUser(ctx, "+27825550101")
	bob, _ := rt.Store.CreateUser(ctx, "+27825550102")
	require.NoError(t, rt.Store.SetOwner(ctx, "d_alice", alice.ID))
	require.NoError(t, rt.Store.SetOwner(ctx, "d_bob", bob.ID))
	require.NoError(t, rt.Store.SetUserStatus(ctx, bob.ID, store.StatusDisabled))

	as := func(u *store.User) func(path string, form url.Values) *http.Response {
		raw, _ := rt.Store.CreateSession(ctx, u.ID, "", time.Hour)
		cookie := &http.Cookie{Name: rt.Auth.CookieName(), Value: raw}
		csrf := csrfForRaw(rt, raw)
		return func(path string, form url.Values) *http.Response {
			form.Set("csrf", csrf)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(cookie)
			resp, err := app.Test(req)
			require.NoError(t, err)
			return resp
		}
	}
	member, super := as(alice), as(admin)

	resp := member("/admin/users/"+bob.ID+"/delete", url.Values{"confirm": {"+27825550102"}})
	require.Equal(t, http.StatusNotFound, resp.StatusCode, "a member cannot delete a disabled account")
	_, err := rt.Store.UserByID(ctx, bob.ID)
	require.NoError(t, err, "Bob is still there")

	resp = super("/admin/users/"+alice.ID+"/delete", url.Values{"confirm": {"+27825550101"}})
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "an active account is disabled first")
	_, err = rt.Store.UserByID(ctx, alice.ID)
	require.NoError(t, err)

	resp = super("/admin/users/"+bob.ID+"/delete", url.Values{"confirm": {"+27825550102"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	_, err = rt.Store.UserByID(ctx, bob.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	_, ok := dm.GetDevice("d_bob")
	require.False(t, ok, "his number is purged")
	_, ok = dm.GetDevice("d_alice")
	require.True(t, ok, "hers is untouched")
	owner, err := rt.Store.OwnerOf(ctx, "d_alice")
	require.NoError(t, err)
	require.Equal(t, alice.ID, owner)
}
