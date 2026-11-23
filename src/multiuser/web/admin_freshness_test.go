package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

// Behaviour: Add user, Get sign-in link and the CLI say the same thing about
// a one-time link.
func TestOneTimeLinksAreDescribedTheSameEverywhere(t *testing.T) {
	const want = "Send them this link. It works once, for 15 minutes."
	require.Equal(t, "It works once, for 15 minutes.", OneTimeLinkNote)
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, otherID := signedIn(t, e, "+27825550101")
	csrf := csrfFor(t, e, admin)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+otherID+"/signin-link", nil)
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(admin)
	_, link := e.do(t, req)
	require.Contains(t, link, want)
	_, added := postAddUser(t, e, admin, csrf, "+15555550123")
	require.Contains(t, added, want)
}

// Behaviour (house rule D4): after Add user, Done lands on the list with the
// new row on top, marked new, whatever the list showed before.
func TestAddUserDoneShowsTheNewRowOnTop(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, _ = signedIn(t, e, "+27825550101")
	_, frag := postAddUser(t, e, admin, csrfFor(t, e, admin), "+15555550123")
	u, err := e.st.UserByPhone(context.Background(), "+15555550123")
	require.NoError(t, err)
	require.Contains(t, frag, `data-reload-on-close="/admin/users?new=`+u.ID+`"`)

	// Even a view that would not show it (sorted by number, then disabled
	// only) pins it first.
	for _, path := range []string{"/admin/users?new=" + u.ID, "/admin/users?sort=phone&dir=desc&new=" + u.ID, "/admin/users?status=disabled&new=" + u.ID} {
		body := get(t, e, admin, path)
		rows := regexp.MustCompile(`<div class="trow[^"]*" role="row" id="user-([^"]+)"`).FindAllStringSubmatch(body, -1)
		require.NotEmpty(t, rows, path)
		require.Equal(t, u.ID, rows[0][1], "%s: the new row is first", path)
		require.Contains(t, rows[0][0], "trow-hl", path)
		require.Contains(t, rowOf(body, u.ID), "Just added", path)
		require.Equal(t, 1, strings.Count(body, `id="user-`+u.ID+`"`), "%s: drawn once", path)
	}
}

// Behaviour: Activity's people link to Users searched by their number, where
// the matching row is highlighted (their row may be on any page).
func TestAPersonLinkFindsTheirRowOnAnyPage(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, adminID := signedIn(t, e, "+27825550100")
	ctx := context.Background()
	var first *store.User
	for i := 0; i < 60; i++ {
		u, err := e.st.CreateUser(ctx, fmt.Sprintf("+2782555%04d", i))
		require.NoError(t, err)
		if i == 0 {
			first = u
		}
	}
	require.NoError(t, e.st.Audit(ctx, store.AuditEvent{UserID: first.ID, ActorID: adminID, Action: "user_disabled"}))
	body := get(t, e, admin, "/admin/activity")
	href := "/admin/users?q=27825550000#user-" + first.ID
	require.Contains(t, body, `href="`+href+`"`)

	list := get(t, e, admin, "/admin/users?q=27825550000")
	require.Contains(t, rowOf(list, first.ID), "trow-hl", "the exact match is highlighted")
	require.NotContains(t, rowOf(list, adminID), "trow-hl")
}

// Behaviour: Activity says "Admin X added Y" for Add user, like its other
// sentences, with numbers written as everywhere else.
func TestActivitySaysWhoAddedWhom(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, _ = postAddUser(t, e, admin, csrfFor(t, e, admin), "+15555550111")
	body := get(t, e, admin, "/admin/activity")
	re := regexp.MustCompile(`Admin <a [^>]*>&#43;27 82 555 0100</a> added <a [^>]*>&#43;1 555 555 0111</a>`)
	require.Regexp(t, re, body)
	require.NotContains(t, body, "user added")
}
