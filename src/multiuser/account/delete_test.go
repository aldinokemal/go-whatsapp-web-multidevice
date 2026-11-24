package account

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// fakeDevices stands in for the inner (unscoped) device usecase. It records
// every call, can be told to fail a device a number of times, and answers
// "not found" for a device it has already removed (as the real one does).
type fakeDevices struct {
	mu      sync.Mutex
	calls   []string
	fail    map[string]int
	removed map[string]bool
}

func (f *fakeDevices) RemoveDevice(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, id)
	if f.fail[id] > 0 {
		f.fail[id]--
		return errors.New("whatsapp store busy")
	}
	if f.removed[id] {
		return pkgError.ErrDeviceNotFound
	}
	f.removed[id] = true
	return nil
}

func (f *fakeDevices) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type fakeGrants struct{ subjects []string }

func (f *fakeGrants) RevokeSubject(_ context.Context, subject string) error {
	f.subjects = append(f.subjects, subject)
	return nil
}

type fakeChats struct{ ids []string }

func (f *fakeChats) DeleteDeviceData(id string) error {
	f.ids = append(f.ids, id)
	return nil
}

type fixture struct {
	st      *store.Store
	d       *Deleter
	devices *fakeDevices
	grants  *fakeGrants
	chats   *fakeChats
	root    string // the media root
	service string // the service device id
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "accounts.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	f := &fixture{
		st:      st,
		devices: &fakeDevices{fail: map[string]int{}, removed: map[string]bool{}},
		grants:  &fakeGrants{},
		chats:   &fakeChats{},
		root:    filepath.Join(t.TempDir(), "media"),
	}
	require.NoError(t, os.MkdirAll(f.root, 0o755))
	f.d = &Deleter{
		Store: st, Devices: f.devices, Grants: f.grants, Chats: f.chats, MediaRoot: f.root,
		ServiceDevice: func(context.Context) string { return f.service },
	}
	return f
}

type person struct {
	user    *store.User
	session string
	apiKey  string
	link    string
	devices []string
}

// seed gives a person one of everything WTF stores about them: a session, a
// passkey, an API key, an unused sign-in link, linked numbers with downloaded
// media, and audit lines carrying their number and IP.
func (f *fixture) seed(t *testing.T, phone string, devices ...string) person {
	t.Helper()
	ctx := context.Background()
	u, err := f.st.CreateUser(ctx, phone)
	require.NoError(t, err)
	sess, err := f.st.CreateSession(ctx, u.ID, "ua", time.Hour)
	require.NoError(t, err)
	require.NoError(t, f.st.AddPasskey(ctx, store.Passkey{CredentialID: "cred_" + u.ID, UserID: u.ID, CredentialJSON: []byte(`{}`)}))
	key, _, err := f.st.CreateAPIKey(ctx, u.ID, "script")
	require.NoError(t, err)
	link, err := f.st.CreateLoginToken(ctx, phone, time.Hour)
	require.NoError(t, err)
	for _, id := range devices {
		require.NoError(t, f.st.SetOwner(ctx, id, u.ID))
		dir := filepath.Join(f.root, id, "2026-11-23")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("jpeg"), 0o600))
		require.NoError(t, f.st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: "device_add", DeviceID: id, IP: "203.0.113.9", Detail: strings.TrimPrefix(phone, "+")}))
	}
	return person{user: u, session: sess, apiKey: key, link: link, devices: devices}
}

// gate is the real sign-in middleware in front of one API route, so a test
// can ask "does this session or key still get in?".
func gate(t *testing.T, st *store.Store) (*auth.Manager, *fiber.App) {
	t.Helper()
	m := auth.New(auth.Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: 30 * 24 * time.Hour, Secure: true}, st)
	app := fiber.New()
	app.Use(m.Middleware(), m.APIKey(), m.RequireAPI())
	app.Get("/devices", func(c fiber.Ctx) error { return c.SendStatus(http.StatusOK) })
	return m, app
}

func getsIn(t *testing.T, m *auth.Manager, app *fiber.App, session, apiKey string) bool {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/devices", nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: m.CookieName(), Value: session})
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp.StatusCode == http.StatusOK
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func auditLines(t *testing.T, st *store.Store, userID, action string) []store.AuditRow {
	t.Helper()
	rows, _, err := st.ListAudit(context.Background(), store.AuditQuery{UserID: userID, Limit: 200})
	require.NoError(t, err)
	var out []store.AuditRow
	for _, r := range rows {
		if action == "" || r.Action == action {
			out = append(out, r)
		}
	}
	return out
}

// Behaviour (and the two-user isolation proof): deleting Alice removes every
// row, number, chat partition, media folder and credential that is hers, and
// nothing of Bob's.
func TestDeleteRemovesEverything(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.seed(t, "+27825550100", "d_a1", "d_a2")
	bob := f.seed(t, "+27825550101", "d_b1")
	m, app := gate(t, f.st)
	require.True(t, getsIn(t, m, app, bob.session, ""))

	require.NoError(t, f.d.Delete(ctx, alice.user.ID))

	// Alice: gone everywhere.
	_, err := f.st.UserByID(ctx, alice.user.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	owned, err := f.st.DevicesOf(ctx, alice.user.ID)
	require.NoError(t, err)
	require.Empty(t, owned)
	pks, err := f.st.PasskeysOf(ctx, alice.user.ID)
	require.NoError(t, err)
	require.Empty(t, pks)
	keys, err := f.st.APIKeysOf(ctx, alice.user.ID)
	require.NoError(t, err)
	require.Empty(t, keys)
	sessions, err := f.st.SessionsOf(ctx, alice.user.ID)
	require.NoError(t, err)
	require.Empty(t, sessions)
	ok, err := f.st.LoginTokenValid(ctx, alice.link)
	require.NoError(t, err)
	require.False(t, ok, "her unused sign-in link is gone")
	require.False(t, getsIn(t, m, app, alice.session, ""))
	require.False(t, getsIn(t, m, app, "", alice.apiKey))
	for _, id := range alice.devices {
		require.False(t, exists(filepath.Join(f.root, id)), "media for %s removed", id)
	}
	require.True(t, exists(f.root), "the media root itself stays")

	// Only Alice's numbers, chat partitions and grants were touched.
	require.ElementsMatch(t, []string{"d_a1", "d_a2"}, f.devices.Calls())
	require.ElementsMatch(t, []string{"d_a1", "d_a2"}, f.chats.ids)
	require.Equal(t, []string{alice.user.ID}, f.grants.subjects)

	// One account_deleted line, naming the user id only; no number or IP
	// survives about her.
	lines := auditLines(t, f.st, alice.user.ID, "account_deleted")
	require.Len(t, lines, 1)
	require.Equal(t, alice.user.ID, lines[0].UserID)
	for _, r := range auditLines(t, f.st, alice.user.ID, "") {
		require.NotContains(t, r.Detail, "27825550100")
		require.Empty(t, r.IP)
	}

	// Bob: untouched.
	require.True(t, getsIn(t, m, app, bob.session, ""))
	require.True(t, getsIn(t, m, app, "", bob.apiKey))
	owned, err = f.st.DevicesOf(ctx, bob.user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"d_b1"}, owned)
	ok, err = f.st.LoginTokenValid(ctx, bob.link)
	require.NoError(t, err)
	require.True(t, ok)
	pks, err = f.st.PasskeysOf(ctx, bob.user.ID)
	require.NoError(t, err)
	require.Len(t, pks, 1)
	require.True(t, exists(filepath.Join(f.root, "d_b1", "2026-11-23", "photo.jpg")))
	bobLines := auditLines(t, f.st, bob.user.ID, "device_add")
	require.Len(t, bobLines, 1)
	require.Equal(t, "27825550101", bobLines[0].Detail)
	require.Equal(t, "203.0.113.9", bobLines[0].IP)
}

// Behaviour: the account that owns the service (sign-in) number cannot be
// deleted, and the refusal changes nothing at all.
func TestOwnsServiceNumberIsRefusedAndChangesNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.seed(t, "+27825550100", "d_a1", "d_svc")
	f.service = "d_svc"
	m, app := gate(t, f.st)

	err := f.d.Delete(ctx, alice.user.ID)
	require.ErrorIs(t, err, ErrOwnsServiceNumber)

	u, err := f.st.UserByID(ctx, alice.user.ID)
	require.NoError(t, err)
	require.Equal(t, store.StatusActive, u.Status)
	require.Empty(t, f.devices.Calls())
	require.Empty(t, f.grants.subjects)
	owned, err := f.st.DevicesOf(ctx, alice.user.ID)
	require.NoError(t, err)
	require.Len(t, owned, 2)
	require.True(t, getsIn(t, m, app, alice.session, ""))
	require.True(t, getsIn(t, m, app, "", alice.apiKey))
	require.True(t, exists(filepath.Join(f.root, "d_a1")))
	require.Empty(t, auditLines(t, f.st, alice.user.ID, "account_deleted"))
}

// Mechanism: a deletion that stops half way leaves an account nobody can use
// (not with an old session, not with a key, not with a passkey sign-in that
// mints a fresh session), and ResumePending finishes it.
func TestInterruptedDeleteIsRefusedThenResumed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.seed(t, "+27825550100", "d_a1", "d_a2")
	bob := f.seed(t, "+27825550101", "d_b1")
	f.devices.fail["d_a2"] = 1
	m, app := gate(t, f.st)

	err := f.d.Delete(ctx, alice.user.ID)
	require.Error(t, err)

	u, err := f.st.UserByID(ctx, alice.user.ID)
	require.NoError(t, err, "the user row is the last thing to go")
	require.Equal(t, store.StatusDeleting, u.Status)
	require.False(t, getsIn(t, m, app, alice.session, ""), "her old session is refused")
	require.False(t, getsIn(t, m, app, "", alice.apiKey), "her key is refused")
	// A passkey still exists until the end; signing in with it would mint a
	// session. That session must be refused too.
	fresh, err := f.st.CreateSession(ctx, alice.user.ID, "ua", time.Hour)
	require.NoError(t, err)
	require.False(t, getsIn(t, m, app, fresh, ""), "a fresh session for a deleting account is refused")
	require.ErrorIs(t, f.st.SetUserStatus(ctx, alice.user.ID, store.StatusActive), store.ErrDeleting, "an admin cannot bring it back")

	require.NoError(t, f.d.ResumePending(ctx))

	_, err = f.st.UserByID(ctx, alice.user.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
	require.Equal(t, []string{"d_a1", "d_a2", "d_a2"}, f.devices.Calls(), "the finished number is not removed twice; the failed one is retried")
	require.False(t, exists(filepath.Join(f.root, "d_a2")))
	require.Len(t, auditLines(t, f.st, alice.user.ID, "account_deleted"), 1)
	require.True(t, getsIn(t, m, app, bob.session, ""), "bob was never affected")
	b, err := f.st.UserByID(ctx, bob.user.ID)
	require.NoError(t, err)
	require.Equal(t, store.StatusActive, b.Status, "resume only finishes accounts already deleting")
}

// Mechanism: every step is safe to repeat. A number the device manager no
// longer has (a ghost slot, or one removed just before a crash) counts as
// done, and deleting an account that is already gone is fine.
func TestDeleteIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.seed(t, "+27825550100", "d_a1", "d_ghost")
	f.devices.removed["d_ghost"] = true

	require.NoError(t, f.d.Delete(ctx, alice.user.ID))
	require.NoError(t, f.d.Delete(ctx, alice.user.ID))
	require.NoError(t, f.d.ResumePending(ctx))

	require.ElementsMatch(t, []string{"d_a1", "d_ghost"}, f.devices.Calls())
	require.ElementsMatch(t, []string{"d_a1", "d_ghost"}, f.chats.ids, "a ghost's chat partition is still cleared")
	require.Len(t, auditLines(t, f.st, alice.user.ID, "account_deleted"), 1)
}

// Mechanism: a start-up resume racing the person's own click writes one
// account_deleted line and removes each number once.
func TestConcurrentDeletesFinishOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	alice := f.seed(t, "+27825550100", "d_a1", "d_a2")
	require.NoError(t, f.st.SetUserStatus(ctx, alice.user.ID, store.StatusDeleting))

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				errs[i] = f.d.Delete(ctx, alice.user.ID)
			} else {
				errs[i] = f.d.ResumePending(ctx)
			}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.ElementsMatch(t, []string{"d_a1", "d_a2"}, f.devices.Calls())
	require.Len(t, auditLines(t, f.st, alice.user.ID, "account_deleted"), 1)
}

// Security: a device id that would put the media path outside the media root
// is refused for the file step (and logged); nothing outside the root is
// deleted, and the rest of the deletion still finishes.
func TestMediaPathMustStayUnderRoot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	outside := filepath.Join(filepath.Dir(f.root), "x")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("not yours"), 0o600))
	alice := f.seed(t, "+27825550100")
	for _, id := range []string{"../x", "..", ".", "a/../../x"} {
		require.NoError(t, f.st.SetOwner(ctx, id, alice.user.ID))
	}

	require.NoError(t, f.d.Delete(ctx, alice.user.ID))

	require.True(t, exists(filepath.Join(outside, "keep.txt")), "nothing outside the media root is deleted")
	require.True(t, exists(f.root), "the media root itself is never deleted")
	_, err := f.st.UserByID(ctx, alice.user.ID)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// Security: a device folder that is a symlink is unlinked, never followed.
func TestMediaSymlinkIsNotFollowed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	require.NoError(t, os.MkdirAll(elsewhere, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(elsewhere, "keep.txt"), []byte("not yours"), 0o600))
	alice := f.seed(t, "+27825550100")
	require.NoError(t, f.st.SetOwner(ctx, "d_link", alice.user.ID))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(f.root, "d_link")))

	require.NoError(t, f.d.Delete(ctx, alice.user.ID))

	require.False(t, exists(filepath.Join(f.root, "d_link")))
	require.True(t, exists(filepath.Join(elsewhere, "keep.txt")))
}

// Behaviour (phase 4): a deleted person leaves no waitlist row behind, and
// nobody else's is touched.
func TestDeleteAccountRemovesWaitlistRow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.seed(t, "+27825550101", "d_a")
	for _, phone := range []string{"+27825550101", "+27825550102"} {
		_, _, err := f.st.JoinWaitlist(ctx, phone)
		require.NoError(t, err)
	}
	_, _, err := f.st.AdmitFromWaitlist(ctx, "+27825550101", "admin", 2)
	require.NoError(t, err)
	require.NoError(t, f.d.Delete(ctx, p.user.ID))
	n, err := f.st.WaitlistCount(ctx, store.WaitlistAdmitted)
	require.NoError(t, err)
	require.Zero(t, n, "their row is gone")
	n, err = f.st.WaitlistCount(ctx, store.WaitlistWaiting)
	require.NoError(t, err)
	require.Equal(t, 1, n, "someone else's place is kept")
}
