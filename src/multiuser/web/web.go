// Package web serves WTF's own pages: sign-in, My WhatsApp, account and admin.
// htmx.min.js is htmx 2.0.4, sha256 e209dda5c8235479f3166defc7750e1dbcd5a5c1808b7792fc2e6733768fb447.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
	"time"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/passkey"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

//go:embed icons/*.svg
var iconsFS embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"ago":   humanAgo,
	"dict":  templateDict,
	"icon":  icon,
	"phone": displayPhone,
	"since": func(t time.Time) string { return humanAgo(&t) },
	// recent is true for a moment within the last ten minutes, for marking
	// something "Just linked" / "Just added" from real timestamps.
	"recent":        func(t time.Time) bool { return !t.IsZero() && time.Since(t) < 10*time.Minute },
	"deleteConfirm": deleteConfirm,
}).ParseFS(templatesFS, "templates/*.html"))

// icon renders one of the vendored Lucide icons (ISC licence, icons/LICENSE-lucide.txt)
// as inline SVG, so it inherits currentColor and needs no extra request. The
// icon is decorative: the control around it carries the accessible name. An
// unknown name is an error, so a typo fails the render (and the tests) rather
// than drawing nothing.
func icon(name string) (template.HTML, error) {
	body, err := iconsFS.ReadFile("icons/" + name + ".svg")
	if err != nil {
		return "", fmt.Errorf("icon %q: %w", name, err)
	}
	return template.HTML(`<svg class="icon" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">` +
		string(body) + `</svg>`), nil
}

// displayPhone shows a stored number the way people read it: "+", the
// country code, then digit groups ("+27 82 555 0100", "+1 555 555 0142").
// Stored values are E.164 ("+27...") for users and bare digits for WhatsApp
// devices; anything that is not 8-15 digits is shown unchanged. Inputs and
// links keep the raw value.
func displayPhone(p string) string {
	digits := strings.TrimPrefix(p, "+")
	if len(digits) < 8 || len(digits) > 15 || strings.Trim(digits, "0123456789") != "" {
		return p
	}
	cc := countryCodeLen(digits)
	national := digits[cc:]
	var groups []string
	if digits[0] == '1' && len(national) == 10 { // NANP: 3-3-4
		groups = []string{national[:3], national[3:6], national[6:]}
	} else {
		head, tail := national[:len(national)-4], national[len(national)-4:]
		for len(head) > 4 {
			groups = append([]string{head[len(head)-3:]}, groups...)
			head = head[:len(head)-3]
		}
		if head != "" {
			groups = append([]string{head}, groups...)
		}
		groups = append(groups, tail)
	}
	return "+" + digits[:cc] + " " + strings.Join(groups, " ")
}

// countryCodeLen is the length of the E.164 country code at the start of
// digits: 1 for +1 and +7, 2 for the two-digit codes, 3 otherwise.
func countryCodeLen(digits string) int {
	switch digits[0] {
	case '1', '7':
		return 1
	}
	two := map[string]bool{"20": true, "27": true, "30": true, "31": true, "32": true, "33": true, "34": true, "36": true,
		"39": true, "40": true, "41": true, "43": true, "44": true, "45": true, "46": true, "47": true, "48": true, "49": true,
		"51": true, "52": true, "53": true, "54": true, "55": true, "56": true, "57": true, "58": true, "60": true, "61": true,
		"62": true, "63": true, "64": true, "65": true, "66": true, "81": true, "82": true, "84": true, "86": true, "90": true,
		"91": true, "92": true, "93": true, "94": true, "95": true, "98": true}
	if two[digits[:2]] {
		return 2
	}
	return 3
}

// templateDict builds a map[string]any from alternating string keys and
// values, so a template can pass a small bundle of named fields into a
// shared sub-template (used to reuse the device-linking fragment between
// home.html and link_modal.html) without a dedicated Go type for each call site.
func templateDict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, errors.New("dict: odd number of arguments")
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, errors.New("dict: keys must be strings")
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}

const csp = "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

type Deps struct {
	Store        *store.Store
	Auth         *auth.Manager
	Passkeys     *passkey.Service
	Devices      domainDevice.IDeviceUsecase
	ProductName  string
	PublicURL    string // for building absolute links, e.g. the admin sign-in link
	ServicePhone func(ctx context.Context) string
	Paired       func() []owners.PairedDevice
	StorageDir   string
	// Grants lists and ends the signed-in user's connected apps (MCP OAuth
	// token families). Nil when MCP is off: the Account page then shows no
	// Connected apps card.
	Grants GrantStore
	// MCPURL is the MCP endpoint people connect Claude to.
	MCPURL string
	// DeleteAccount deletes an account and everything tied to it
	// (account.Deleter.Delete). Nil hides Delete account.
	DeleteAccount func(ctx context.Context, userID string) error
	// SendWhatsApp sends text from the service number to phone (E.164), in
	// the chat that person opened with it. Nil: Let in shows the link to copy.
	SendWhatsApp func(ctx context.Context, phone, text string) error
	// Capacity says how full the server is. Nil: no cap, no capacity card.
	Capacity CapacityReader
	// DefaultMaxDevices is a new account's own limit (WTF_DEFAULT_MAX_DEVICES).
	DefaultMaxDevices int
}

// GrantStore is the part of the MCP OAuth server the Account page uses.
type GrantStore interface {
	ListGrants(ctx context.Context, subject string) ([]oauth.Grant, error)
	RevokeGrant(ctx context.Context, subject, familyID string) error
	// RevokeSubject ends every grant of the subject (a user id).
	RevokeSubject(ctx context.Context, subject string) error
}

// page is the data every template gets.
type page struct {
	Product    string
	Title      string
	User       *store.User
	Superadmin bool
	CSRF       string
	Path       string // the request path, for marking the current menu entry
	Waiting    int    // people on the waitlist, for the menu's count (superadmins)
	PublicURL  string // absolute base for links that leave the site (og:image)
	Data       any
}

func (d Deps) render(c fiber.Ctx, status int, name, title string, data any) error {
	return d.renderCSP(c, status, name, title, data, csp)
}

func (d Deps) renderCSP(c fiber.Ctx, status int, name, title string, data any, policy string) error {
	u, _ := auth.UserFrom(c.Context())
	p := page{Product: d.ProductName, Title: title, User: u, Superadmin: d.Auth.IsSuperadmin(u), CSRF: d.Auth.CSRFToken(c), Path: c.Path(), PublicURL: strings.TrimRight(d.PublicURL, "/"), Data: data}
	if p.Superadmin {
		// Derived at render time (house rule T2); a failed count just hides it.
		p.Waiting, _ = d.Store.WaitlistCount(c.Context(), store.WaitlistWaiting)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, p); err != nil {
		return err
	}
	c.Set(fiber.HeaderContentSecurityPolicy, policy)
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Referrer-Policy", "same-origin")
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Type("html", "utf-8")
	return c.Status(status).Send(buf.Bytes())
}

func userOf(c fiber.Ctx) (*store.User, bool) { return auth.UserFrom(c.Context()) }

func Mount(app *fiber.App, d Deps) {
	static, _ := fs.Sub(staticFS, "static")
	app.Get("/ui/static/*", func(c fiber.Ctx) error {
		name := c.Params("*")
		b, err := fs.ReadFile(static, name)
		if err != nil {
			return fiber.ErrNotFound
		}
		cache := "public, max-age=3600"
		if strings.HasPrefix(name, "brand/") {
			cache = brandCache
		}
		c.Set(fiber.HeaderCacheControl, cache)
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set(fiber.HeaderContentType, staticType(name))
		return c.Send(b)
	})
	mountPWA(app, d)
	mountSignin(app, d)
	mountDevices(app, d) // Task 10
	mountAccount(app, d) // Task 11
	mountAccountDelete(app, d)
	mountAdmin(app, d) // Task 11
	mountAdminManage(app, d)
	mountActivity(app, d)
	mountWaitlist(app, d)
	mountSessions(app, d)
	mountWebhook(app, d)
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// machinePrefixes answer in JSON. Everything else is a page a person reads.
var machinePrefixes = []string{"/auth/passkey/", "/ui/devices/", "/app/", "/send/", "/user/", "/message/",
	"/chat/", "/chats", "/group/", "/newsletter/", "/devices", "/call/", "/schedule", "/mcp", "/health",
	"/files/", "/oauth/token", "/oauth/register", "/.well-known/"}

func IsMachinePath(path string) bool {
	for _, p := range machinePrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func ErrorHandler(d Deps) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		msg := "Something went wrong at our end."
		var fe *fiber.Error
		if errors.As(err, &fe) {
			code = fe.Code
			msg = fe.Message
			if code == fiber.StatusNotFound {
				msg = "We couldn't find that page."
			}
		} else {
			logrus.WithError(err).Errorf("[WTF] unhandled on %s", c.Path())
		}
		if IsMachinePath(c.Path()) {
			return c.Status(code).JSON(fiber.Map{"status": code, "code": "ERROR", "message": msg})
		}
		return d.render(c, code, "error.html", "Error", map[string]any{"Message": msg, "Code": code})
	}
}

// humanAgo renders a friendly relative time, or "never" for a nil moment.
func humanAgo(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "never"
	}
	d := time.Since(*t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		n := int(d / time.Minute)
		if n == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", n)
	case d < 24*time.Hour:
		n := int(d / time.Hour)
		if n == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", n)
	default:
		n := int(d / (24 * time.Hour))
		if n == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", n)
	}
}
