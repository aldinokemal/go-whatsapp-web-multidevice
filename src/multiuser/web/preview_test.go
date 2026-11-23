package web

// The preview harness renders every page and state of the web UI, with
// realistic fixture data, into a static site that a browser can load, plus a
// manifest of the screenshots to take. It is skipped unless WTF_PREVIEW_DIR is
// set. See docs/ui.md for how to run it and shoot the screenshots.

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/stretchr/testify/require"
)

// previewShot is one screenshot the sweep takes: a page, optionally with a
// dialog opened and some JS-only text (errors, copy feedback) filled in.
type previewShot struct {
	Name    string            `json:"name"`
	Page    string            `json:"page"`
	Dialog  string            `json:"dialog,omitempty"`
	Set     map[string]string `json:"set,omitempty"`     // CSS selector -> text
	Attr    string            `json:"attr,omitempty"`    // attribute set on <html>, e.g. data-nav-open
	Storage map[string]string `json:"storage,omitempty"` // localStorage set before the page loads
}

type previewSite struct {
	t     *testing.T
	dir   string
	shots []previewShot
}

func (p *previewSite) write(name, html string) {
	p.t.Helper()
	require.NoError(p.t, os.WriteFile(filepath.Join(p.dir, "pages", name+".html"), []byte(html), 0o644))
}

func (p *previewSite) shot(s previewShot) { p.shots = append(p.shots, s) }

func (p *previewSite) file(rel string, b []byte) {
	p.t.Helper()
	full := filepath.Join(p.dir, filepath.FromSlash(rel))
	require.NoError(p.t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(p.t, os.WriteFile(full, b, 0o644))
}

var dialogBodyRE = regexp.MustCompile(`(<div id="link-dialog-body"[^>]*>)(\s*</div>)`)
var addUserBodyRE = regexp.MustCompile(`(?s)(<div id="add-user-body"[^>]*>)(.*?)(</div>\s*</div>\s*</dialog>)`)

// withAddUserBody replaces the Add user modal's content with a response.
func withAddUserBody(page, fragment string) string {
	return addUserBodyRE.ReplaceAllString(page, "${1}"+strings.ReplaceAll(fragment, "$", "$$")+"${3}")
}

// withDialogBody puts an htmx fragment where the real page would swap it in.
func withDialogBody(page, fragment string) string {
	return dialogBodyRE.ReplaceAllString(page, "${1}"+strings.ReplaceAll(fragment, "$", "$$")+"${2}")
}

func get(t *testing.T, e *env, ck *http.Cookie, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ck != nil {
		req.AddCookie(ck)
	}
	_, body := e.do(t, req)
	return body
}

func htmxPost(t *testing.T, e *env, ck *http.Cookie, csrf, path string, form url.Values) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", csrf)
	req.Header.Set("HX-Request", "true")
	req.AddCookie(ck)
	_, body := e.do(t, req)
	return body
}

func TestPreviewSite(t *testing.T) {
	out := os.Getenv("WTF_PREVIEW_DIR")
	if out == "" {
		t.Skip("set WTF_PREVIEW_DIR to render the preview site")
	}
	out, err := filepath.Abs(out)
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(out))
	require.NoError(t, os.MkdirAll(filepath.Join(out, "pages"), 0o755))
	p := &previewSite{t: t, dir: out}

	// Static assets, served at the same paths the real app uses.
	static, _ := fs.Sub(staticFS, "static")
	require.NoError(t, fs.WalkDir(static, ".", func(path string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return err
		}
		b, err := fs.ReadFile(static, path)
		if err != nil {
			return err
		}
		p.file("ui/static/"+path, b)
		return nil
	}))

	// QR images at the URLs the templates point at.
	signinQR, err := qrcode.Encode("https://wa.me/15555550000?text=Sign+me+in+to+WTF", qrcode.Medium, 320)
	require.NoError(t, err)
	p.file("signin/qr.png", signinQR)
	linkQR, err := qrcode.Encode("2@k3Jd8vQm1xYpLr0aZt7Nf2Wc9Hs4Ue6Bg5Ri1Ok8Ml3Pj0Tn,AbCdEfGhIjKlMnOpQrStUvWxYz0123456789=,", qrcode.Medium, 280)
	require.NoError(t, err)
	for _, id := range []string{"d_new", "d_slot"} {
		p.file("ui/devices/"+id+"/qr.png", linkQR)
	}

	ctx := context.Background()

	// --- Signed-out pages -------------------------------------------------
	e := newEnv(t)
	e.d.ProductName = "WTF"
	p.write("signin", get(t, e, nil, "/signin"))
	p.shot(previewShot{Name: "signin", Page: "signin"})
	p.shot(previewShot{Name: "signin-passkey-error", Page: "signin",
		Set: map[string]string{"[data-passkey-error]": "That passkey didn't work. Try again or continue with WhatsApp."}})

	noSvc := newEnv(t)
	noSvc.d.ServicePhone = func(context.Context) string { return "" }
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(noSvc.d)})
	app.Use(noSvc.d.Auth.Middleware())
	Mount(app, noSvc.d)
	noSvc.app = app
	p.write("signin-not-set-up", get(t, noSvc, nil, "/signin"))
	p.shot(previewShot{Name: "signin-not-set-up", Page: "signin-not-set-up"})

	_, _ = e.st.CreateUser(ctx, "+15555550123")
	raw, _ := e.st.CreateLoginToken(ctx, "+15555550123", 15*time.Minute)
	p.write("verify", get(t, e, nil, "/signin/verify?t="+url.QueryEscape(raw)))
	p.shot(previewShot{Name: "verify", Page: "verify"})
	p.write("verify-expired", get(t, e, nil, "/signin/verify?t=expired"))
	p.shot(previewShot{Name: "verify-expired", Page: "verify-expired"})

	// --- Signed-in pages --------------------------------------------------
	// Each person may link two numbers (the store's default), so the fixtures
	// spread devices across people the way real accounts would look.
	stub := &devStub{devices: []domainDevice.Device{
		{ID: "d_work", DisplayName: "Acme Support", PhoneNumber: "15555550142", State: domainDevice.DeviceStateLoggedIn, CreatedAt: time.Now().Add(-9 * 24 * time.Hour)},
		{ID: "d_personal", PhoneNumber: "15555550177", State: domainDevice.DeviceStateDisconnected, CreatedAt: time.Now().Add(-3 * 24 * time.Hour)},
		{ID: "d_slot", CreatedAt: time.Now().Add(-5 * time.Minute)},
		{ID: "d_fresh", DisplayName: "Sales", PhoneNumber: "447700900123", State: domainDevice.DeviceStateLoggedIn, CreatedAt: time.Now().Add(-2 * time.Minute)},
	}}
	e = withDevices(t, stub)
	e.d.Devices = owners.NewScopedDevices(stub, e.st)
	appWithBoom := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	appWithBoom.Use(e.d.Auth.Middleware())
	appWithBoom.Get("/boom", func(fiber.Ctx) error { return errors.New("preview: forced failure") })
	Mount(appWithBoom, e.d)
	appWithBoom.Use(e.d.Auth.RequireAPI())
	e.app = appWithBoom

	// Spread sign-ins over time so "last sign-in" reads realistically.
	base := time.Now().UTC()
	at := func(d time.Duration) { e.st.Now = func() time.Time { return base.Add(-d) } }
	at(26 * time.Hour)
	other, otherID := signedIn(t, e, "+15555550188")
	at(3 * time.Hour)
	_, disabledID := signedIn(t, e, "+15555550199")
	at(40 * time.Minute)
	fresh, freshID := signedIn(t, e, "+15555550111")
	at(0)
	admin, adminID := signedIn(t, e, "+27825550100")
	e.st.Now = func() time.Time { return time.Now().UTC() }
	require.NoError(t, e.st.SetUserStatus(ctx, disabledID, store.StatusDisabled))
	require.NoError(t, e.st.SetOwner(ctx, "d_work", adminID))
	require.NoError(t, e.st.SetOwner(ctx, "d_personal", adminID))
	require.NoError(t, e.st.SetOwner(ctx, "d_slot", otherID))
	require.NoError(t, e.st.SetOwner(ctx, "d_fresh", freshID))
	csrf := csrfFor(t, e, admin)

	// The admin is at their two-number limit: no Link button, a notice.
	homeLimit := get(t, e, admin, "/")
	p.write("home-at-limit", homeLimit)
	p.shot(previewShot{Name: "home-at-limit", Page: "home-at-limit"})
	p.shot(previewShot{Name: "home-unlink-dialog", Page: "home-at-limit", Dialog: "remove-d_work"})
	p.shot(previewShot{Name: "home-menu-open", Page: "home-at-limit", Attr: "data-nav-open"})
	p.shot(previewShot{Name: "home-rail", Page: "home-at-limit", Attr: "data-rail"})

	// A member with one unfinished link; another with a number linked just now.
	home := get(t, e, other, "/")
	p.write("home", home)
	p.shot(previewShot{Name: "home", Page: "home"})
	p.write("home-just-linked", get(t, e, fresh, "/"))
	p.shot(previewShot{Name: "home-just-linked", Page: "home-just-linked"})

	// The real AddDevice refusal (the admin is at the limit) is the error state.
	linkErr := htmxPost(t, e, admin, csrf, "/ui/devices", nil)
	linkFrag := tmplString(t, "link_modal.html", map[string]any{"ID": "d_new"})
	p.write("home-link", withDialogBody(home, linkFrag))
	// Default mode: QR on a computer, pairing code on a phone.
	p.shot(previewShot{Name: "home-link-dialog", Page: "home-link", Dialog: "link-dialog"})
	p.write("home-link-qr", withDialogBody(home, strings.Replace(linkFrag, "data-link-modes", `data-link-modes data-mode="qr"`, 1)))
	p.shot(previewShot{Name: "home-link-qr", Page: "home-link-qr", Dialog: "link-dialog"})
	p.write("home-link-pair-form", withDialogBody(home, strings.Replace(linkFrag, "data-link-modes", `data-link-modes data-mode="pair"`, 1)))
	p.shot(previewShot{Name: "home-link-pair-form", Page: "home-link-pair-form", Dialog: "link-dialog"})
	// Before WhatsApp has produced a code the QR request fails: a spinner shows.
	p.write("home-link-qr-loading", withDialogBody(home, strings.Replace(strings.ReplaceAll(linkFrag, "/ui/devices/d_new/qr.png", "/ui/devices/d_pending/qr.png"), "data-link-modes", `data-link-modes data-mode="qr"`, 1)))
	p.shot(previewShot{Name: "home-link-qr-loading", Page: "home-link-qr-loading", Dialog: "link-dialog"})
	pair := htmxPost(t, e, other, csrfFor(t, e, other), "/ui/devices/d_slot/pair-code", url.Values{"phone": {"+15555550142"}})
	p.write("home-link-pair-code", withDialogBody(home, pair))
	p.shot(previewShot{Name: "home-link-pair-code", Page: "home-link-pair-code", Dialog: "link-dialog"})
	p.write("home-link-error", withDialogBody(homeLimit, linkErr))
	p.shot(previewShot{Name: "home-link-error", Page: "home-link-error", Dialog: "link-dialog"})

	// Home with nothing linked yet: a brand-new member.
	empty, _ := signedIn(t, e, "+15555550155")
	p.write("home-empty", get(t, e, empty, "/"))
	p.shot(previewShot{Name: "home-empty", Page: "home-empty"})

	// Account, with passkeys and without.
	used := time.Now().Add(-2 * time.Hour)
	require.NoError(t, e.st.AddPasskey(ctx, store.Passkey{CredentialID: "cred_mac", UserID: adminID, Name: "Safari on macOS",
		CredentialJSON: []byte(`{}`), LastUsedAt: &used}))
	require.NoError(t, e.st.AddPasskey(ctx, store.Passkey{CredentialID: "cred_phone", UserID: adminID, Name: "Chrome on Android",
		CredentialJSON: []byte(`{}`)}))
	account := get(t, e, admin, "/account")
	p.write("account", account)
	p.shot(previewShot{Name: "account", Page: "account"})
	p.shot(previewShot{Name: "account-signout-everywhere", Page: "account", Dialog: "signout-everywhere"})
	p.shot(previewShot{Name: "account-remove-passkey", Page: "account", Dialog: "remove-passkey-cred_mac"})
	p.shot(previewShot{Name: "account-passkey-error", Page: "account",
		Set: map[string]string{"[data-passkey-register-error]": "Adding the passkey was cancelled."}})
	p.shot(previewShot{Name: "account-theme-light", Page: "account", Storage: map[string]string{"wtf-theme": "light"}})
	p.shot(previewShot{Name: "account-theme-dark", Page: "account", Storage: map[string]string{"wtf-theme": "dark"}})
	p.write("account-empty", get(t, e, empty, "/account"))
	p.shot(previewShot{Name: "account-empty", Page: "account-empty"})

	// Admin users: one just added from the Add user modal (never signed in).
	usersBefore := get(t, e, admin, "/admin/users")
	p.write("admin-add-user", usersBefore)
	p.shot(previewShot{Name: "admin-add-user", Page: "admin-add-user", Dialog: "add-user"})
	dup := htmxPost(t, e, admin, csrf, "/admin/users", url.Values{"phone": {"+1 555 555 0188"}})
	p.write("admin-add-user-error", withAddUserBody(usersBefore, dup))
	p.shot(previewShot{Name: "admin-add-user-error", Page: "admin-add-user-error", Dialog: "add-user"})
	added := htmxPost(t, e, admin, csrf, "/admin/users", url.Values{"phone": {"+44 7700 900456"}})
	p.write("admin-add-user-done", withAddUserBody(usersBefore, added))
	p.shot(previewShot{Name: "admin-add-user-done", Page: "admin-add-user-done", Dialog: "add-user"})
	users := get(t, e, admin, "/admin/users")
	p.write("admin-users", users)
	p.shot(previewShot{Name: "admin-users", Page: "admin-users"})
	p.shot(previewShot{Name: "admin-users-disable", Page: "admin-users", Dialog: "disable-" + otherID})
	link := htmxPost(t, e, admin, csrf, "/admin/users/"+otherID+"/signin-link", nil)
	p.write("admin-users-link", withDialogBody(users, link))
	p.shot(previewShot{Name: "admin-users-link", Page: "admin-users-link", Dialog: "link-dialog",
		Set: map[string]string{"[data-copy-feedback]": "Copied."}})

	// Admin settings, with linked numbers and without.
	require.NoError(t, e.st.SetSetting(ctx, store.SettingServiceDeviceID, "d_work", adminID))
	p.write("admin-settings", get(t, e, admin, "/admin/settings"))
	p.shot(previewShot{Name: "admin-settings", Page: "admin-settings"})
	freshAdminEnv := withDevices(t, &devStub{})
	freshAdmin, _ := signedIn(t, freshAdminEnv, "+27825550100")
	p.write("admin-settings-empty", get(t, freshAdminEnv, freshAdmin, "/admin/settings"))
	p.shot(previewShot{Name: "admin-settings-empty", Page: "admin-settings-empty"})

	// Offline page (served by the service worker when the network is gone).
	p.write("offline", get(t, e, nil, "/offline"))
	p.shot(previewShot{Name: "offline", Page: "offline"})

	// Error pages.
	p.write("error-404", get(t, e, admin, "/no-such-page"))
	p.shot(previewShot{Name: "error-404", Page: "error-404"})
	p.write("error-500", get(t, e, admin, "/boom"))
	p.shot(previewShot{Name: "error-500", Page: "error-500"})

	b, err := json.MarshalIndent(p.shots, "", "  ")
	require.NoError(t, err)
	p.file("manifest.json", b)
	t.Logf("preview site written to %s (%d shots)", out, len(p.shots))
}

// tmplString renders a fragment template directly, for states the stub
// cannot produce through a route (e.g. AddDevice refusing a new slot).
func tmplString(t *testing.T, name string, data any) string {
	t.Helper()
	var sb strings.Builder
	require.NoError(t, tmpl.ExecuteTemplate(&sb, name, page{Product: "WTF", CSRF: "preview", Data: data}))
	return sb.String()
}
