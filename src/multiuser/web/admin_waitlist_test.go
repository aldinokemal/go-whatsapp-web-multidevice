package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/capacity"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

// sentMessages records what the service number would send.
type sentMessages struct {
	mu   sync.Mutex
	to   []string
	text []string
	err  error
}

func (s *sentMessages) Send(_ context.Context, phone, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.to, s.text = append(s.to, phone), append(s.text, text)
	return nil
}

// fakeCapacity is a fixed reading.
type fakeCapacity struct{ c capacity.Capacity }

func (f fakeCapacity) Snapshot(context.Context) (capacity.Capacity, error) { return f.c, nil }
func (f fakeCapacity) Full(context.Context) bool                           { return f.c.Max > 0 && f.c.Linked >= f.c.Max }

func waitlistEnv(t *testing.T, sent *sentMessages, cap CapacityReader) *env {
	t.Helper()
	e := newEnv(t)
	e.d.SendWhatsApp = sent.Send
	e.d.Capacity = cap
	e.d.DefaultMaxDevices = 3
	remount(e, &devStub{})
	return e
}

// join puts phones on the waitlist a minute apart, oldest first.
func join(t *testing.T, st *store.Store, phones ...string) {
	t.Helper()
	base := time.Now().UTC().Add(-time.Duration(len(phones)+1) * time.Hour)
	for i, p := range phones {
		st.Now = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		_, _, err := st.JoinWaitlist(context.Background(), p)
		require.NoError(t, err)
	}
	st.Now = func() time.Time { return time.Now().UTC() }
}

func waitlistPost(t *testing.T, e *env, ck *http.Cookie, csrf, path string) (*http.Response, string) {
	t.Helper()
	form := url.Values{"csrf": {csrf}}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.AddCookie(ck)
	return e.do(t, req)
}

// Security: every waitlist route is a 404 to a member, like a page that
// does not exist, and needs no less than a superadmin.
func TestWaitlistIs404ForMembers(t *testing.T) {
	sent := &sentMessages{}
	e := waitlistEnv(t, sent, nil)
	join(t, e.st, "+27825550111")
	member, _ := signedIn(t, e, "+27825550101")
	csrf := csrfFor(t, e, member)
	req := httptest.NewRequest(http.MethodGet, "/admin/waitlist", nil)
	req.AddCookie(member)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	for _, a := range []string{"admit", "decline", "reopen", "remove"} {
		resp, _ := waitlistPost(t, e, member, csrf, "/admin/waitlist/27825550111/"+a)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, a)
	}
	require.Empty(t, sent.to)
	_, err := e.st.UserByPhone(context.Background(), "+27825550111")
	require.ErrorIs(t, err, store.ErrNotFound)
	require.NotContains(t, get(t, e, member, "/"), `href="/admin/waitlist"`, "members see no Waitlist entry")
}

// Behaviour: waiting people are listed oldest first with their place; the
// count equals the list and the nav pill equals the count.
func TestWaitlistListsWaitingInOrder(t *testing.T) {
	e := waitlistEnv(t, &sentMessages{}, nil)
	join(t, e.st, "+27825550111", "+27825550112", "+27825550113")
	require.NoError(t, e.st.DeclineWaitlist(context.Background(), "+27825550113", "x"))
	admin, _ := signedIn(t, e, "+27825550100")
	body := get(t, e, admin, "/admin/waitlist")
	require.Contains(t, body, "2 waiting")
	i1, i2 := strings.Index(body, "&#43;27 82 555 0111"), strings.Index(body, "&#43;27 82 555 0112")
	require.True(t, i1 > 0 && i2 > i1, "oldest first")
	require.NotContains(t, body, "&#43;27 82 555 0113", "declined is its own tab")
	require.Regexp(t, `(?s)class="nav-label">Waitlist</span><span class="nav-count"[^>]*>2</span>`, body)

	declined := get(t, e, admin, "/admin/waitlist?status=declined")
	require.Contains(t, declined, "1 declined")
	require.Contains(t, declined, "&#43;27 82 555 0113")
	require.Contains(t, declined, "Move back to waiting")
}

// Behaviour (house rule T3): 50 a page, the count is every match.
func TestWaitlistPagesFifty(t *testing.T) {
	e := waitlistEnv(t, &sentMessages{}, nil)
	var phones []string
	for i := 0; i < 120; i++ {
		phones = append(phones, fmt.Sprintf("+2782556%04d", i))
	}
	join(t, e.st, phones...)
	admin, _ := signedIn(t, e, "+27825550100")
	row := regexp.MustCompile(`class="trow wl-row"`)
	body := get(t, e, admin, "/admin/waitlist")
	require.Contains(t, body, "120 waiting")
	require.Len(t, row.FindAllString(body, -1), 50)
	require.Contains(t, body, `href="/admin/waitlist?page=2"`)
	body = get(t, e, admin, "/admin/waitlist?page=3")
	require.Len(t, row.FindAllString(body, -1), 20)
	require.Contains(t, body, ">101<", "positions carry on across pages")
}

// Behaviour: Let in creates the account with the default limit, sends the
// sign-in link once from the service number, and swaps the row.
func TestLetInCreatesSendsAndSwapsTheRow(t *testing.T) {
	sent := &sentMessages{}
	e := waitlistEnv(t, sent, nil)
	join(t, e.st, "+27825550111")
	admin, adminID := signedIn(t, e, "+27825550100")
	resp, body := waitlistPost(t, e, admin, csrfFor(t, e, admin), "/admin/waitlist/27825550111/admit")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, `id="wl-27825550111"`)
	require.Contains(t, body, "Link sent on WhatsApp")
	require.Equal(t, []string{"+27825550111"}, sent.to)
	require.Contains(t, sent.text[0], "https://wtf.onrender.com/signin/verify?t=")
	require.Contains(t, sent.text[0], "WTF")
	u, err := e.st.UserByPhone(context.Background(), "+27825550111")
	require.NoError(t, err)
	require.Equal(t, 3, u.MaxDevices)
	rows, _, _ := e.st.ListAudit(context.Background(), store.AuditQuery{Limit: 10})
	require.Equal(t, "waitlist_admitted", rows[0].Action)
	require.Equal(t, adminID, rows[0].ActorID)
	require.NotContains(t, rows[0].Detail, "27825550111", "no phone in the audit row")
}

// Behaviour (Review Focus 3): a second Let in, from another tab, sends
// nothing and creates nothing.
func TestLetInTwiceSendsOnce(t *testing.T) {
	sent := &sentMessages{}
	e := waitlistEnv(t, sent, nil)
	join(t, e.st, "+27825550111")
	admin, _ := signedIn(t, e, "+27825550100")
	csrf := csrfFor(t, e, admin)
	waitlistPost(t, e, admin, csrf, "/admin/waitlist/27825550111/admit")
	_, body := waitlistPost(t, e, admin, csrf, "/admin/waitlist/27825550111/admit")
	require.Len(t, sent.to, 1)
	require.Contains(t, body, "already has an account")
}

// Behaviour: when WhatsApp will not send, the admin gets the link to copy.
func TestLetInWhenSendFailsShowsTheLink(t *testing.T) {
	sent := &sentMessages{err: errors.New("service number not connected")}
	e := waitlistEnv(t, sent, nil)
	join(t, e.st, "+27825550111")
	admin, _ := signedIn(t, e, "+27825550100")
	_, body := waitlistPost(t, e, admin, csrfFor(t, e, admin), "/admin/waitlist/27825550111/admit")
	require.Contains(t, body, "Couldn't send on WhatsApp")
	require.Contains(t, body, "/signin/verify?t=")
	require.Contains(t, body, "data-copy=")
}

// Behaviour (V1): a full server hides Let in, says so, and refuses a post.
func TestLetInHiddenWhenFull(t *testing.T) {
	sent := &sentMessages{}
	e := waitlistEnv(t, sent, fakeCapacity{capacity.Capacity{Linked: 20, Max: 20}})
	join(t, e.st, "+27825550111")
	admin, _ := signedIn(t, e, "+27825550100")
	body := get(t, e, admin, "/admin/waitlist")
	require.Contains(t, body, "The server is full")
	require.NotContains(t, body, "/admin/waitlist/27825550111/admit")
	require.Contains(t, body, "20 of 20 numbers linked")
	_, frag := waitlistPost(t, e, admin, csrfFor(t, e, admin), "/admin/waitlist/27825550111/admit")
	require.Contains(t, frag, "The server is full")
	require.Empty(t, sent.to)
	_, err := e.st.UserByPhone(context.Background(), "+27825550111")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Behaviour: Decline, Move back to waiting and Remove, none of which sends
// anything.
func TestDeclineReopenRemove(t *testing.T) {
	sent := &sentMessages{}
	e := waitlistEnv(t, sent, nil)
	join(t, e.st, "+27825550111")
	admin, _ := signedIn(t, e, "+27825550100")
	csrf := csrfFor(t, e, admin)
	ctx := context.Background()
	status := func() string {
		_, items, _ := e.st.WaitlistPage(ctx, store.WaitlistDeclined, 0, 10)
		if len(items) == 1 {
			return items[0].Status
		}
		_, items, _ = e.st.WaitlistPage(ctx, store.WaitlistWaiting, 0, 10)
		if len(items) == 1 {
			return items[0].Status
		}
		return "gone"
	}
	resp, body := waitlistPost(t, e, admin, csrf, "/admin/waitlist/27825550111/decline")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "Declined")
	require.Equal(t, store.WaitlistDeclined, status())
	_, body = waitlistPost(t, e, admin, csrf, "/admin/waitlist/27825550111/reopen")
	require.Contains(t, body, "Back to waiting")
	require.Equal(t, store.WaitlistWaiting, status())
	_, body = waitlistPost(t, e, admin, csrf, "/admin/waitlist/27825550111/remove")
	require.Contains(t, body, "Removed")
	require.Equal(t, "gone", status())
	require.Empty(t, sent.to)

	resp, _ = waitlistPost(t, e, admin, csrf, "/admin/waitlist/not-a-number/remove")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// Security: every waitlist POST needs the CSRF token.
func TestWaitlistPostsNeedCSRF(t *testing.T) {
	sent := &sentMessages{}
	e := waitlistEnv(t, sent, nil)
	join(t, e.st, "+27825550111")
	admin, _ := signedIn(t, e, "+27825550100")
	for _, a := range []string{"admit", "decline", "reopen", "remove"} {
		resp, _ := waitlistPost(t, e, admin, "wrong", "/admin/waitlist/27825550111/"+a)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, a)
	}
	require.Empty(t, sent.to)
}

// Behaviour: nobody waiting is an empty state, and no pill in the menu.
func TestWaitlistEmpty(t *testing.T) {
	e := waitlistEnv(t, &sentMessages{}, nil)
	admin, _ := signedIn(t, e, "+27825550100")
	body := get(t, e, admin, "/admin/waitlist")
	require.Contains(t, body, "Nobody is waiting.")
	require.NotContains(t, body, `class="nav-count"`)
}
