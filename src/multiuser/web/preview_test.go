package web

// The preview harness renders every page and state of the web UI, with
// realistic fixture data, into a static site that a browser can load, plus a
// manifest of the screenshots to take. It is skipped unless WTF_PREVIEW_DIR is
// set. See docs/ui.md for how to run it and shoot the screenshots.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/capacity"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
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
	Click   []string          `json:"click,omitempty"`   // selectors clicked in order (a kebab, then an item)
	// ViewportOnly shoots the visible screen rather than the full page (an open menu or dialog).
	ViewportOnly bool `json:"viewportOnly,omitempty"`
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

var newKeyBodyRE = regexp.MustCompile(`(?s)(<div id="new-key-body"[^>]*>)(.*?)(</div>\s*</div>\s*</dialog>)`)

// withNewKeyBody replaces the New API key modal's content with a response.
func withNewKeyBody(page, fragment string) string {
	return newKeyBodyRE.ReplaceAllString(page, "${1}"+strings.ReplaceAll(fragment, "$", "$$")+"${3}")
}

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
	off, _ := e.st.CreateUser(ctx, "+15555550124")
	rawOff, _ := e.st.CreateLoginToken(ctx, "+15555550124", 15*time.Minute)
	require.NoError(t, e.st.SetUserStatus(ctx, off.ID, store.StatusDisabled))
	p.write("verify-disabled", get(t, e, nil, "/signin/verify?t="+url.QueryEscape(rawOff)))
	p.shot(previewShot{Name: "verify-disabled", Page: "verify-disabled"})

	// --- Signed-in pages --------------------------------------------------
	// Each person may link two numbers (the store's default), so the fixtures
	// spread devices across people the way real accounts would look.
	stub := &devStub{devices: []domainDevice.Device{
		{ID: "d_work", DisplayName: "Acme Support", PhoneNumber: "15555550142", State: domainDevice.DeviceStateLoggedIn, CreatedAt: time.Now().Add(-9 * 24 * time.Hour)},
		{ID: "d_personal", PhoneNumber: "15555550177", State: domainDevice.DeviceStateDisconnected, CreatedAt: time.Now().Add(-3 * 24 * time.Hour)},
		{ID: "d_slot", CreatedAt: time.Now().Add(-5 * time.Minute)},
		{ID: "d_fresh", DisplayName: "Sales", PhoneNumber: "447700900123", State: domainDevice.DeviceStateLoggedIn, CreatedAt: time.Now().Add(-2 * time.Minute)},
		{ID: "d_out", CreatedAt: time.Now().Add(-20 * 24 * time.Hour)},
		{ID: "d_dup", CreatedAt: time.Now().Add(-2 * time.Minute)},
		{ID: "d_other", DisplayName: "Bean There Cafe", PhoneNumber: "15555550190", State: domainDevice.DeviceStateLoggedIn, CreatedAt: time.Now().Add(-12 * 24 * time.Hour)},
		{ID: "d_other_out", CreatedAt: time.Now().Add(-30 * 24 * time.Hour)},
	}}
	e = withDevices(t, stub)
	e.d.Devices = owners.NewScopedDevices(stub, e.st)
	grants := &fakeGrants{grants: map[string][]oauth.Grant{}}
	e.d.Grants, e.d.MCPURL = grants, "https://wtf.onrender.com/mcp"
	// Phases 3 and 4: Delete account, Let in from the waitlist, capacity.
	e.d.DeleteAccount = func(context.Context, string) error { return nil }
	sent := &sentMessages{}
	e.d.SendWhatsApp = sent.Send
	e.d.Capacity = capacity.Service{Store: e.st, MaxPhones: 20}
	e.d.DefaultMaxDevices = 2
	// Phase 3 retention at its production default: My WhatsApp says so.
	e.d.RetentionHours = 24
	// People waiting, so the admin's menu shows the count everywhere.
	join(t, e.st, "+447700900311", "+15555550312", "+27825550313", "+5511955550314", "+15555550315", "+919855550316")
	appWithBoom := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	appWithBoom.Use(e.d.Auth.Middleware())
	appWithBoom.Get("/boom", func(fiber.Ctx) error { return errors.New("preview: forced failure") })
	Mount(appWithBoom, e.d)
	MountDeviceLabelPage(appWithBoom, e.d)
	appWithBoom.Use(NotFoundPages(appWithBoom, e.d)) // as the runtime installs it
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
	require.NoError(t, e.st.SetOwner(ctx, "d_other", otherID))
	// Gap A and B states: a number the phone signed out, and a refused duplicate.
	for _, id := range []string{"d_out", "d_dup", "d_other_out"} {
		require.NoError(t, e.st.SetOwner(ctx, id, otherID))
	}
	require.NoError(t, e.st.MarkSignedOut(ctx, "d_out", "15555550166", "Front desk"))
	require.NoError(t, e.st.RecordRefusal(ctx, "d_dup", store.RefusalDuplicate))
	require.NoError(t, e.st.MarkSignedOut(ctx, "d_other_out", "15555550191", ""))
	require.NoError(t, e.st.SetMaxDevices(ctx, otherID, 6))
	csrf := csrfFor(t, e, admin)

	// The admin is at their two-number limit: no Link button, a notice.
	homeLimit := get(t, e, admin, "/")
	p.write("home-at-limit", homeLimit)
	p.shot(previewShot{Name: "home-at-limit", Page: "home-at-limit"})
	p.shot(previewShot{Name: "home-unlink-dialog", Page: "home-at-limit", Dialog: "remove-d_work"})
	p.shot(previewShot{Name: "home-menu-open", Page: "home-at-limit", Attr: "data-nav-open"})
	p.shot(previewShot{Name: "home-rail", Page: "home-at-limit", Attr: "data-rail"})

	// Reconnect, drawn in place: checking, then a way to link again.
	rowRE := regexp.MustCompile(`(?s)<li class="row" id="row-d_personal".*?</li>`)
	checking := get(t, e, admin, "/ui/devices/d_personal/row?n=1")
	p.write("home-reconnecting", rowRE.ReplaceAllLiteralString(homeLimit, strings.ReplaceAll(checking, `hx-trigger="load delay:3s"`, "")))
	p.shot(previewShot{Name: "home-reconnecting", Page: "home-reconnecting"})
	failed := get(t, e, admin, "/ui/devices/d_personal/row?n=5")
	p.write("home-reconnect-failed", rowRE.ReplaceAllLiteralString(homeLimit, failed))
	p.shot(previewShot{Name: "home-reconnect-failed", Page: "home-reconnect-failed"})

	// A member with one unfinished link; another with a number linked just now.
	home := get(t, e, other, "/")
	p.write("home", home)
	p.shot(previewShot{Name: "home", Page: "home"})
	p.shot(previewShot{Name: "retention-home", Page: "home"})
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
	p.write("home-link-expired", withDialogBody(home, strings.Replace(linkFrag, "data-link-session", `data-link-session data-ended="expired"`, 1)))
	p.shot(previewShot{Name: "home-link-expired", Page: "home-link-expired", Dialog: "link-dialog"})
	p.write("home-link-failed", withDialogBody(home, strings.Replace(linkFrag, "data-link-session", `data-link-session data-ended="failed"`, 1)))
	p.shot(previewShot{Name: "home-link-failed", Page: "home-link-failed", Dialog: "link-dialog"})
	pair := htmxPost(t, e, other, csrfFor(t, e, other), "/ui/devices/d_slot/pair-code", url.Values{"phone": {"+15555550142"}})
	p.write("home-link-pair-code", withDialogBody(home, pair))
	p.shot(previewShot{Name: "home-link-pair-code", Page: "home-link-pair-code", Dialog: "link-dialog"})
	p.write("home-link-error", withDialogBody(homeLimit, linkErr))
	p.shot(previewShot{Name: "home-link-error", Page: "home-link-error", Dialog: "link-dialog"})

	// Per-number webhook dialog: empty, saved, refused address.
	asWebhook := func(page string) string {
		return strings.Replace(page, `id="link-dialog-title">Link WhatsApp<`, `id="link-dialog-title">Webhook<`, 1)
	}
	hook := get(t, e, admin, "/ui/devices/d_work/webhook")
	p.write("home-webhook", withDialogBody(asWebhook(homeLimit), hook))
	p.shot(previewShot{Name: "home-webhook", Page: "home-webhook", Dialog: "link-dialog"})
	hookSaved := tmplString(t, "webhook_form.html", map[string]any{"ID": "d_work", "URL": "https://hooks.acme.example/whatsapp", "HasSecret": true, "Saved": "Saved."})
	p.write("home-webhook-saved", withDialogBody(asWebhook(homeLimit), hookSaved))
	p.shot(previewShot{Name: "home-webhook-saved", Page: "home-webhook-saved", Dialog: "link-dialog"})
	hookErr := htmxPost(t, e, admin, csrf, "/ui/devices/d_work/webhook", url.Values{"url": {"https://127.0.0.1/hook"}})
	p.write("home-webhook-error", withDialogBody(asWebhook(homeLimit), hookErr))
	p.shot(previewShot{Name: "home-webhook-error", Page: "home-webhook-error", Dialog: "link-dialog"})

	// Home with nothing linked yet: a brand-new member.
	empty, _ := signedIn(t, e, "+15555550155")
	p.write("home-empty", get(t, e, empty, "/"))
	p.shot(previewShot{Name: "home-empty", Page: "home-empty"})
	p.shot(previewShot{Name: "retention-home-empty", Page: "home-empty"})

	// Account, with passkeys and without.
	used := time.Now().Add(-2 * time.Hour)
	require.NoError(t, e.st.AddPasskey(ctx, store.Passkey{CredentialID: "cred_mac", UserID: adminID, Name: "Safari on macOS",
		CredentialJSON: []byte(`{}`), LastUsedAt: &used}))
	require.NoError(t, e.st.AddPasskey(ctx, store.Passkey{CredentialID: "cred_phone", UserID: adminID, Name: "Chrome on Android",
		CredentialJSON: []byte(`{}`)}))
	at(5 * 24 * time.Hour)
	_, _ = e.st.CreateSession(ctx, adminID, "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1", 30*24*time.Hour)
	at(26 * time.Hour)
	_, _ = e.st.CreateSession(ctx, adminID, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36 Edg/131.0", 30*24*time.Hour)
	at(5 * 24 * time.Hour)
	_, laptopKey, err := e.st.CreateAPIKey(ctx, adminID, "Laptop script")
	require.NoError(t, err)
	at(3 * time.Hour)
	require.NoError(t, e.st.TouchAPIKey(ctx, laptopKey.ID))
	at(20 * time.Minute)
	_, _, err = e.st.CreateAPIKey(ctx, adminID, "Claude Code")
	require.NoError(t, err)
	e.st.Now = func() time.Time { return time.Now().UTC() }
	grants.grants[adminID] = []oauth.Grant{
		{FamilyID: "gowa_family_web", ClientHost: "claude.ai", ClientName: "Claude", CreatedAt: time.Now().Add(-6 * 24 * time.Hour), LastUsedAt: time.Now().Add(-25 * time.Minute)},
		{FamilyID: "gowa_family_code", ClientHost: "claude.ai", ClientName: "Claude Code", CreatedAt: time.Now().Add(-2 * time.Hour), LastUsedAt: time.Now().Add(-2 * time.Hour)},
	}
	account := get(t, e, admin, "/account")
	p.write("account", account)
	p.shot(previewShot{Name: "account", Page: "account"})
	p.shot(previewShot{Name: "account-signout-everywhere", Page: "account", Dialog: "signout-everywhere"})
	p.shot(previewShot{Name: "account-signout-session", Page: "account", ViewportOnly: true,
		Click: []string{`[data-open-dialog="signout-session"]`}})
	p.shot(previewShot{Name: "account-remove-passkey", Page: "account", Dialog: "remove-passkey-cred_mac"})
	p.shot(previewShot{Name: "account-passkey-error", Page: "account",
		Set: map[string]string{"[data-passkey-register-error]": "Adding the passkey was cancelled."}})
	p.shot(previewShot{Name: "account-theme-light", Page: "account", Storage: map[string]string{"wtf-theme": "light"}})
	p.shot(previewShot{Name: "account-theme-dark", Page: "account", Storage: map[string]string{"wtf-theme": "dark"}})
	p.shot(previewShot{Name: "account-revoke-key", Page: "account", ViewportOnly: true,
		Click: []string{`[data-open-dialog="revoke-key"]`}})
	p.shot(previewShot{Name: "account-new-key", Page: "account", Dialog: "new-key"})
	keyErr := htmxPost(t, e, admin, csrf, "/account/api-keys", url.Values{"name": {"   "}})
	p.write("account-new-key-error", withNewKeyBody(account, keyErr))
	p.shot(previewShot{Name: "account-new-key-error", Page: "account-new-key-error", Dialog: "new-key"})
	keyDone := htmxPost(t, e, admin, csrf, "/account/api-keys", url.Values{"name": {"Home server"}})
	require.Contains(t, keyDone, store.APIKeyPrefix)
	p.write("account-new-key-done", withNewKeyBody(account, keyDone))
	p.shot(previewShot{Name: "account-new-key-done", Page: "account-new-key-done", Dialog: "new-key"})
	p.write("account-empty", get(t, e, empty, "/account"))
	p.shot(previewShot{Name: "account-empty", Page: "account-empty"})

	// Connected apps: the Connect Claude dialog, and Disconnect asking first.
	p.shot(previewShot{Name: "account-connect-claude", Page: "account", Dialog: "connect-claude"})
	p.shot(previewShot{Name: "account-connect-claude-copied", Page: "account", Dialog: "connect-claude",
		Set: map[string]string{"#connect-claude .connect-step:nth-of-type(2) [data-copy-feedback]": "Copied."}})
	p.shot(previewShot{Name: "account-disconnect-app", Page: "account", ViewportOnly: true,
		Click: []string{`[data-open-dialog="disconnect-app"]`}})

	// The OAuth consent page (GET /oauth/authorize, signed in): a web
	// client, a client on this computer (Claude Code), and a refused request.
	consentApp := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	consentApp.Use(e.d.Auth.Middleware())
	hidden := map[string]string{"response_type": "code", "client_id": "https://claude.ai/oauth/mcp-oauth-client-metadata",
		"redirect_uri": "https://claude.ai/api/mcp/auth_callback", "code_challenge": "x", "code_challenge_method": "S256",
		"resource": "https://wtf.onrender.com/mcp", "scope": "mcp", "state": "s"}
	consentApp.Get("/consent", func(c fiber.Ctx) error {
		return RenderStandalone(c, e.d, "consent.html", "Allow access", oauth.ConsentView{ClientHost: "claude.ai", ClientName: "Claude",
			RedirectHost: "claude.ai", Action: "/oauth/authorize", Hidden: hidden, CSRF: "csrf"}, "https://claude.ai")
	})
	consentApp.Get("/consent-loopback", func(c fiber.Ctx) error {
		return RenderStandalone(c, e.d, "consent.html", "Allow access", oauth.ConsentView{ClientHost: "claude.ai", ClientName: "Claude Code",
			RedirectHost: "localhost:54321", LoopbackOnly: true, Action: "/oauth/authorize", Hidden: hidden, CSRF: "csrf"}, "http://localhost:54321")
	})
	consentApp.Get("/consent-error", func(c fiber.Ctx) error {
		return RenderError(c, e.d, fiber.StatusBadRequest, "This request to connect an app isn't valid.")
	})
	consentEnv := &env{app: consentApp, st: e.st, d: e.d}
	for _, name := range []string{"consent", "consent-loopback", "consent-error"} {
		p.write(name, get(t, consentEnv, admin, "/"+name))
		p.shot(previewShot{Name: name, Page: name})
	}

	// Admin users: one just added from the Add user modal (never signed in).
	usersBefore := get(t, e, admin, "/admin/users")
	p.write("admin-add-user", usersBefore)
	p.shot(previewShot{Name: "admin-add-user", Page: "admin-add-user", Dialog: "add-user"})
	dup := htmxPost(t, e, admin, csrf, "/admin/users", url.Values{"phone": {"+1 555 555 0188"}})
	p.write("admin-add-user-error", withAddUserBody(usersBefore, dup))
	p.shot(previewShot{Name: "admin-add-user-error", Page: "admin-add-user-error", Dialog: "add-user"})
	added := htmxPost(t, e, admin, csrf, "/admin/users", url.Values{"phone": {"+44 7700 900456"}})
	addedUser, err := e.st.UserByPhone(ctx, "+447700900456")
	require.NoError(t, err)
	p.write("admin-add-user-done", withAddUserBody(usersBefore, added))
	p.shot(previewShot{Name: "admin-add-user-done", Page: "admin-add-user-done", Dialog: "add-user"})
	users := get(t, e, admin, "/admin/users")
	p.write("admin-users", users)
	p.shot(previewShot{Name: "admin-users", Page: "admin-users"})
	p.shot(previewShot{Name: "admin-users-disable", Page: "admin-users", Dialog: "disable-" + otherID})
	row := "#user-" + otherID + " "
	p.shot(previewShot{Name: "admin-users-expanded", Page: "admin-users", Click: []string{row + "[data-disclosure]"}})
	p.shot(previewShot{Name: "admin-users-menu", Page: "admin-users", ViewportOnly: true, Click: []string{row + "[data-menu-trigger]"}})
	p.shot(previewShot{Name: "admin-users-limit", Page: "admin-users", ViewportOnly: true,
		Click: []string{row + "[data-menu-trigger]", row + `[data-open-dialog="limit-dialog"]`}})
	p.shot(previewShot{Name: "admin-users-phone", Page: "admin-users", ViewportOnly: true,
		Click: []string{row + "[data-menu-trigger]", row + `[data-open-dialog="phone-dialog"]`}})
	p.shot(previewShot{Name: "admin-users-unlink", Page: "admin-users", ViewportOnly: true,
		Click: []string{row + "[data-disclosure]", "#numbers-" + otherID + ` [data-open-dialog="admin-unlink"]`}})
	// Done after Add user: the list with the new row pinned and highlighted.
	p.write("admin-users-new", get(t, e, admin, "/admin/users?sort=phone&new="+addedUser.ID))
	p.shot(previewShot{Name: "admin-users-new-row", Page: "admin-users-new"})
	// A disabled user's ⋯ menu ends with Delete account, and its dialog.
	delRow := "#user-" + disabledID + " "
	p.shot(previewShot{Name: "admin-users-delete-menu", Page: "admin-users", ViewportOnly: true, Click: []string{delRow + "[data-menu-trigger]"}})
	p.shot(previewShot{Name: "admin-users-delete-dialog", Page: "admin-users", ViewportOnly: true,
		Click: []string{delRow + "[data-menu-trigger]", delRow + `[data-open-dialog="delete-` + disabledID + `"]`}})
	delWrong := htmxPost(t, e, admin, csrf, "/admin/users/"+disabledID+"/delete", url.Values{"confirm": {"DELETE"}})
	delPage := regexp.MustCompile(`(?s)(<dialog id="delete-`+disabledID+`"[^>]*>).*?(</dialog>)`).
		ReplaceAllString(users, "${1}"+strings.ReplaceAll(delWrong, "$", "$$")+"${2}")
	p.write("admin-users-delete-error", delPage)
	p.shot(previewShot{Name: "admin-users-delete-error", Page: "admin-users-delete-error", Dialog: "delete-" + disabledID, ViewportOnly: true})
	// From Activity: the person's number searched, their row highlighted.
	p.write("admin-users-from-activity", get(t, e, admin, "/admin/users?q=15555550188"))
	p.shot(previewShot{Name: "admin-users-from-activity", Page: "admin-users-from-activity"})
	p.write("admin-users-error", postPage(t, e, admin, csrf, "/admin/users/"+otherID+"/phone", url.Values{"phone": {"+27825550100"}}))
	p.shot(previewShot{Name: "admin-users-error", Page: "admin-users-error"})

	// Activity: a realistic mix, then the same filtered to one user, then empty.
	for _, ev := range []store.AuditEvent{
		{UserID: otherID, Action: "signin"},
		{UserID: otherID, Action: "device_add", DeviceID: "d_other"},
		{UserID: disabledID, Action: "device_link_refused_duplicate", DeviceID: "d_x"},
		{UserID: otherID, ActorID: adminID, Action: "user_limit_set", Detail: "3"},
		{UserID: disabledID, ActorID: adminID, Action: "user_disabled"},
		{UserID: otherID, ActorID: adminID, Action: "device_remove", DeviceID: "d_y", Detail: "15555550193"},
		{UserID: otherID, ActorID: adminID, Action: "user_phone_changed", Detail: "+15555550187 +15555550188"},
		{UserID: adminID, Action: "passkey_add"},
		{UserID: adminID, Action: "webhook_set", DeviceID: "d_work"},
	} {
		require.NoError(t, e.st.Audit(ctx, ev))
	}
	p.write("admin-activity", get(t, e, admin, "/admin/activity"))
	p.shot(previewShot{Name: "admin-activity", Page: "admin-activity"})
	p.write("admin-activity-user", get(t, e, admin, "/admin/activity?user="+otherID))
	p.shot(previewShot{Name: "admin-activity-user", Page: "admin-activity-user"})
	link := htmxPost(t, e, admin, csrf, "/admin/users/"+otherID+"/signin-link", nil)
	p.write("admin-users-link", withDialogBody(users, link))
	p.shot(previewShot{Name: "admin-users-link", Page: "admin-users-link", Dialog: "link-dialog",
		Set: map[string]string{"[data-copy-feedback]": "Copied."}})

	// Admin settings: no sign-in number yet, one set, and one set but down.
	p.write("admin-settings-unset", get(t, e, admin, "/admin/settings"))
	p.shot(previewShot{Name: "admin-settings-unset", Page: "admin-settings-unset"})
	require.NoError(t, e.st.SetSetting(ctx, store.SettingServiceDeviceID, "d_work", adminID))
	p.write("admin-settings", get(t, e, admin, "/admin/settings"))
	p.shot(previewShot{Name: "admin-settings", Page: "admin-settings"})
	// The owner of the sign-in number is told why there's no Delete button.
	p.write("account-delete-service", get(t, e, admin, "/account"))
	p.shot(previewShot{Name: "account-delete-service", Page: "account-delete-service"})
	freshAdminEnv := withDevices(t, &devStub{})
	freshAdmin, _ := signedIn(t, freshAdminEnv, "+27825550100")
	p.write("admin-settings-empty", get(t, freshAdminEnv, freshAdmin, "/admin/settings"))
	p.shot(previewShot{Name: "admin-settings-empty", Page: "admin-settings-empty"})

	previewPhase34(t, p, e, admin, adminID, csrf, other, otherID, sent)

	// Offline page (served by the service worker when the network is gone).
	p.write("offline", get(t, e, nil, "/offline"))
	p.shot(previewShot{Name: "offline", Page: "offline"})

	// Error pages.
	p.write("error-404", get(t, e, admin, "/no-such-page"))
	p.shot(previewShot{Name: "error-404", Page: "error-404"})
	p.write("error-404-signed-out", get(t, e, nil, "/nope"))
	p.shot(previewShot{Name: "error-404-signed-out", Page: "error-404-signed-out"})
	p.write("error-500", get(t, e, admin, "/boom"))
	p.shot(previewShot{Name: "error-500", Page: "error-500"})

	// Sign-in is down: the saved sign-in number is not connected.
	e.d.ServicePhone = func(context.Context) string { return "" }
	remount(e, stub)
	p.write("admin-settings-down", get(t, e, admin, "/admin/settings"))
	p.shot(previewShot{Name: "admin-settings-down", Page: "admin-settings-down"})
	p.write("home-signin-down", get(t, e, admin, "/"))
	p.shot(previewShot{Name: "home-signin-down", Page: "home-signin-down"})

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

// postPage posts a plain form (no htmx) and returns the page it answers with.
func postPage(t *testing.T, e *env, ck *http.Cookie, csrf, path string, form url.Values) string {
	t.Helper()
	form.Set("csrf", csrf)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	_, body := e.do(t, req)
	return body
}

var deleteDialogRE = regexp.MustCompile(`(?s)(<dialog id="delete-account"[^>]*>).*?(</dialog>)`)

// withDeleteForm puts the Delete account dialog's htmx answer in its place.
func withDeleteForm(page, fragment string) string {
	return deleteDialogRE.ReplaceAllString(page, "${1}"+strings.ReplaceAll(fragment, "$", "$$")+"${2}")
}

var oobStartRE = regexp.MustCompile(`<(p|div|a)\b[^>]*\bid="([^"]+)"[^>]*\bhx-swap-oob="true"`)

// withOOB does what htmx does with an answer's out-of-band elements: each
// replaces the page's element with the same id. Elements are taken to end
// where the next one starts (or at the answer's end).
func withOOB(page, answer string) string {
	locs := oobStartRE.FindAllStringSubmatchIndex(answer, -1)
	for i, l := range locs {
		end := len(answer)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		el := strings.TrimSpace(answer[l[0]:end])
		el = strings.Replace(el, ` hx-swap-oob="true"`, "", 1)
		tag, id := answer[l[2]:l[3]], answer[l[4]:l[5]]
		re := regexp.MustCompile(`(?s)<` + tag + `\b[^>]*\bid="` + regexp.QuoteMeta(id) + `".*?</` + tag + `>`)
		if tag == "div" { // the switcher: its own div holds one more
			re = regexp.MustCompile(`(?s)<div\b[^>]*\bid="` + regexp.QuoteMeta(id) + `".*?</select>.*?</div>\s*</div>\s*</div>`)
		}
		page = re.ReplaceAllLiteralString(page, el)
	}
	return page
}

// rowOnly is an htmx row answer without its out-of-band elements.
func rowOnly(answer string) string {
	if l := oobStartRE.FindStringIndex(answer); l != nil {
		return answer[:l[0]]
	}
	return answer
}

// withWaitlistRow swaps one waitlist row for the row an action answered:
// from its opening tag to the next row, or the end of the table.
func withWaitlistRow(page, digits, row string) string {
	start := strings.Index(page, `id="wl-`+digits+`"`)
	if start < 0 {
		return page
	}
	start = strings.LastIndex(page[:start], "<div")
	end := len(page)
	for _, stop := range []string{"\n<div class=\"trow wl-row", "\n  </div>\n"} {
		if i := strings.Index(page[start+1:], stop); i >= 0 && start+1+i < end {
			end = start + 1 + i
		}
	}
	return page[:start] + strings.TrimSpace(rowOnly(row)) + page[end:]
}

// previewPhase34 renders the phase 3 and 4 screens: Delete account, Rename,
// the waitlist, users at scale and the capacity tiles.
func previewPhase34(t *testing.T, p *previewSite, e *env, admin *http.Cookie, adminID, csrf string, other *http.Cookie, otherID string, sent *sentMessages) {
	ctx := context.Background()

	// --- Delete account (phase 3, task 5) ---------------------------------
	otherCSRF := csrfFor(t, e, other)
	acct := get(t, e, other, "/account")
	p.write("account-delete", acct)
	p.shot(previewShot{Name: "account-delete-card", Page: "account-delete"})
	p.shot(previewShot{Name: "account-delete-dialog", Page: "account-delete", Dialog: "delete-account"})
	wrong := htmxPost(t, e, other, otherCSRF, "/account/delete", url.Values{"confirm": {"yes please"}})
	p.write("account-delete-error", withDeleteForm(acct, wrong))
	p.shot(previewShot{Name: "account-delete-error", Page: "account-delete-error", Dialog: "delete-account"})
	busy := strings.Replace(withDeleteForm(acct, tmplString(t, "delete_account_form.html", &deleteView{Numbers: 5})),
		`class="dialog-panel" aria-live`, `class="dialog-panel htmx-request" aria-live`, 1)
	busy = strings.Replace(busy, `name="confirm" type="text"`, `name="confirm" type="text" value="DELETE"`, 1)
	p.write("account-delete-busy", busy)
	p.shot(previewShot{Name: "account-delete-busy", Page: "account-delete-busy", Dialog: "delete-account"})
	p.write("signin-deleted", get(t, e, nil, "/signin?deleted=1"))
	p.shot(previewShot{Name: "signin-deleted", Page: "signin-deleted"})

	// --- Rename a number (phase 3, task 7) --------------------------------
	require.NoError(t, e.st.SetLabel(ctx, adminID, "d_work", "Front desk"))
	homeNamed := get(t, e, admin, "/")
	p.write("home-named", homeNamed)
	p.shot(previewShot{Name: "home-named", Page: "home-named"})
	p.shot(previewShot{Name: "home-row-menu", Page: "home-named", ViewportOnly: true, Click: []string{`#row-d_work [data-menu-trigger]`}})
	p.shot(previewShot{Name: "home-row-menu-disconnected", Page: "home-named", ViewportOnly: true, Click: []string{`#row-d_personal [data-menu-trigger]`}})
	asRename := func(page string) string {
		return strings.Replace(page, `id="link-dialog-title">Link WhatsApp<`, `id="link-dialog-title">Rename<`, 1)
	}
	p.write("home-rename", withDialogBody(asRename(homeNamed), get(t, e, admin, "/ui/devices/d_work/label")))
	p.shot(previewShot{Name: "home-rename", Page: "home-rename", Dialog: "link-dialog"})
	p.write("home-rename-new", withDialogBody(asRename(homeNamed), get(t, e, admin, "/ui/devices/d_personal/label")))
	p.shot(previewShot{Name: "home-rename-unnamed", Page: "home-rename-new", Dialog: "link-dialog"})
	renameErr := htmxPost(t, e, admin, csrf, "/ui/devices/d_work/label", url.Values{"label": {strings.Repeat("Reception and deliveries ", 3)}})
	p.write("home-rename-error", withDialogBody(asRename(homeNamed), renameErr))
	p.shot(previewShot{Name: "home-rename-error", Page: "home-rename-error", Dialog: "link-dialog"})
	require.NoError(t, e.st.SetLabel(ctx, adminID, "d_work", ""))

	// --- Waitlist (phase 4, task 3) ---------------------------------------
	_, _, _ = e.st.JoinWaitlist(ctx, "+15555550312") // asked again
	wl := get(t, e, admin, "/admin/waitlist")
	p.write("admin-waitlist", wl)
	p.shot(previewShot{Name: "admin-waitlist", Page: "admin-waitlist"})
	p.shot(previewShot{Name: "admin-waitlist-let-in-dialog", Page: "admin-waitlist", ViewportOnly: true,
		Click: []string{`#wl-447700900311 [data-open-dialog="admit-dialog"]`}})
	p.shot(previewShot{Name: "admin-waitlist-menu", Page: "admin-waitlist", ViewportOnly: true, Click: []string{`#wl-15555550312 [data-menu-trigger]`}})
	p.shot(previewShot{Name: "admin-waitlist-decline-dialog", Page: "admin-waitlist", ViewportOnly: true,
		Click: []string{`#wl-15555550312 [data-menu-trigger]`, `#wl-15555550312 [data-open-dialog="decline-dialog"]`}})
	admitted := htmxPost(t, e, admin, csrf, "/admin/waitlist/447700900311/admit", nil)
	require.Contains(t, admitted, "Link sent")
	sent.err = errors.New("service number not connected")
	failed := htmxPost(t, e, admin, csrf, "/admin/waitlist/15555550312/admit", nil)
	sent.err = nil
	declined := htmxPost(t, e, admin, csrf, "/admin/waitlist/27825550313/decline", nil)
	after := withWaitlistRow(withWaitlistRow(withWaitlistRow(wl, "447700900311", admitted), "15555550312", failed), "27825550313", declined)
	after = withOOB(after, declined) // the counts the last action redrew
	p.write("admin-waitlist-after", after)
	p.shot(previewShot{Name: "admin-waitlist-after-actions", Page: "admin-waitlist-after"})
	p.write("admin-waitlist-declined", get(t, e, admin, "/admin/waitlist?status=declined"))
	p.shot(previewShot{Name: "admin-waitlist-declined", Page: "admin-waitlist-declined"})

	full := withDevices(t, &devStub{})
	full.d.Capacity = fakeCapacity{capacity.Capacity{Linked: 20, Max: 20, Users: 14, Waiting: 3}}
	remount(full, &devStub{})
	fullAdmin, _ := signedIn(t, full, "+27825550100")
	join(t, full.st, "+447700900411", "+15555550412", "+27825550413")
	p.write("admin-waitlist-full", get(t, full, fullAdmin, "/admin/waitlist"))
	p.shot(previewShot{Name: "admin-waitlist-full", Page: "admin-waitlist-full"})

	many := withDevices(t, &devStub{})
	manyAdmin, _ := signedIn(t, many, "+27825550100")
	var phones []string
	for i := 0; i < 57; i++ {
		phones = append(phones, fmt.Sprintf("+2782557%04d", i))
	}
	join(t, many.st, phones...)
	p.write("admin-waitlist-page-2", get(t, many, manyAdmin, "/admin/waitlist?page=2"))
	p.shot(previewShot{Name: "admin-waitlist-page-2", Page: "admin-waitlist-page-2"})
	manyUsers(t, many.st, 64)
	p.write("admin-users-paged", get(t, many, manyAdmin, "/admin/users?sort=phone&page=2"))
	p.shot(previewShot{Name: "admin-users-paged", Page: "admin-users-paged"})

	emptyAdmin := withDevices(t, &devStub{})
	ea, _ := signedIn(t, emptyAdmin, "+27825550100")
	p.write("admin-waitlist-empty", get(t, emptyAdmin, ea, "/admin/waitlist"))
	p.shot(previewShot{Name: "admin-waitlist-empty", Page: "admin-waitlist-empty"})

	// --- Users at scale (phase 4, task 5) ---------------------------------
	p.write("admin-users-search", get(t, e, admin, "/admin/users?q=555+01"))
	p.shot(previewShot{Name: "admin-users-search", Page: "admin-users-search"})
	p.write("admin-users-no-match", get(t, e, admin, "/admin/users?q=4321"))
	p.shot(previewShot{Name: "admin-users-no-match", Page: "admin-users-no-match"})
	p.write("admin-users-disabled", get(t, e, admin, "/admin/users?status=disabled"))
	p.shot(previewShot{Name: "admin-users-disabled", Page: "admin-users-disabled"})

	// --- Capacity (phase 4, task 6) ---------------------------------------
	near := withDevices(t, &devStub{})
	near.d.Capacity = fakeCapacity{capacity.Capacity{Linked: 18, Max: 20, Users: 14, Waiting: 3}}
	remount(near, &devStub{})
	nearAdmin, _ := signedIn(t, near, "+27825550100")
	p.write("admin-settings-near-full", get(t, near, nearAdmin, "/admin/settings"))
	p.shot(previewShot{Name: "admin-settings-near-full", Page: "admin-settings-near-full"})
	_ = otherID
}
