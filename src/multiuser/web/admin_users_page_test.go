package web

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

var userRowRE = regexp.MustCompile(`<div class="trow" role="row" id="user-`)

// manyUsers adds n members a minute apart (newest last), numbered from
// +27825560000.
func manyUsers(t *testing.T, st *store.Store, n int) {
	t.Helper()
	base := time.Now().UTC().Add(-time.Duration(n+10) * time.Minute)
	for i := 0; i < n; i++ {
		st.Now = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		_, err := st.CreateUser(context.Background(), fmt.Sprintf("+2782556%04d", i))
		require.NoError(t, err)
	}
	st.Now = func() time.Time { return time.Now().UTC() }
}

// Behaviour (house rule T3): 50 rows a page, the count is every match, and
// the pager keeps going.
func TestUsersPageIsPaged(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	manyUsers(t, e.st, 120) // 121 with the admin
	body := get(t, e, admin, "/admin/users")
	require.Len(t, userRowRE.FindAllString(body, -1), 50)
	require.Contains(t, body, "121 users")
	require.Contains(t, body, `href="/admin/users?page=2"`)
	body = get(t, e, admin, "/admin/users?page=3")
	require.Len(t, userRowRE.FindAllString(body, -1), 21)
}

// Behaviour: the search matches phone digits anywhere in the number, in any
// spelling; letters alone match nothing and say so.
func TestUsersSearchByDigits(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	manyUsers(t, e.st, 30)
	body := get(t, e, admin, "/admin/users?q=556+0017")
	require.Len(t, userRowRE.FindAllString(body, -1), 1)
	require.Contains(t, body, "1 user")
	require.Contains(t, body, "&#43;27 82 556 0017")
	require.Contains(t, body, `value="556 0017"`, "what was typed stays in the box")

	body = get(t, e, admin, "/admin/users?q=abc")
	require.Empty(t, userRowRE.FindAllString(body, -1))
	require.Contains(t, body, "No users match")
}

// Behaviour: the status filter, a sort and the search all survive paging
// and each other.
func TestUsersFiltersAreKept(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	manyUsers(t, e.st, 120)
	body := get(t, e, admin, "/admin/users?q=556&status=active&sort=phone")
	require.Contains(t, body, `href="/admin/users?q=556&amp;status=active&amp;sort=phone&amp;page=2"`)
	require.Contains(t, body, `href="/admin/users?q=556&amp;sort=phone"`, "the All filter keeps the search and sort")
	require.Contains(t, body, `href="/admin/users?q=556&amp;status=active&amp;sort=phone&amp;dir=desc"`, "the active sort header flips")

	u, err := e.st.UserByPhone(context.Background(), "+27825560005")
	require.NoError(t, err)
	require.NoError(t, e.st.SetUserStatus(context.Background(), u.ID, store.StatusDisabled))
	body = get(t, e, admin, "/admin/users?status=disabled")
	require.Len(t, userRowRE.FindAllString(body, -1), 1)
	require.Contains(t, body, "1 user")
}

// Behaviour (house rule T7): headers sort ascending then descending; the
// sorted one is marked.
func TestUsersSortHeadersToggle(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	manyUsers(t, e.st, 3)
	order := func(body string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`class="cell-phone">([^<]+)<`).FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}
	asc := get(t, e, admin, "/admin/users?sort=phone")
	require.Contains(t, asc, `aria-sort="ascending"`)
	require.Equal(t, []string{"&#43;27 82 555 0100", "&#43;27 82 556 0000", "&#43;27 82 556 0001", "&#43;27 82 556 0002"}, order(asc))
	desc := get(t, e, admin, "/admin/users?sort=phone&dir=desc")
	require.Contains(t, desc, `aria-sort="descending"`)
	require.Equal(t, "&#43;27 82 556 0002", order(desc)[0])
	def := get(t, e, admin, "/admin/users")
	require.Equal(t, "&#43;27 82 555 0100", order(def)[0], "newest first by default")
}

// Security: a crafted sort key falls back to the default and never errors.
func TestUsersSortKeyIsAllowlisted(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	body := get(t, e, admin, "/admin/users?sort="+strings.ReplaceAll("id; DROP TABLE users", " ", "%20")+"&dir=sideways")
	require.Contains(t, body, "1 user")
	require.NotContains(t, body, "DROP")
}

// Shape (house rule T8): the search types ahead without an Apply button:
// debounced in app.js, stale requests replaced, Enter still submits.
func TestUsersSearchTypesAhead(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	body := get(t, e, admin, "/admin/users")
	form := regexp.MustCompile(`(?s)<form[^>]*class="list-search"[^>]*>.*?</form>`).FindString(body)
	require.NotEmpty(t, form)
	require.Contains(t, form, `hx-sync="this:replace"`)
	require.Contains(t, form, `data-typeahead`)
	require.Contains(t, form, `type="search"`)
	require.NotContains(t, form, `type="submit"`, "no Apply button")
	js, _ := staticFS.ReadFile("static/app.js")
	require.Contains(t, string(js), "data-typeahead")
}
