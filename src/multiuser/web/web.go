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
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"ago":  humanAgo,
	"dict": templateDict,
}).ParseFS(templatesFS, "templates/*.html"))

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
	ServicePhone func(ctx context.Context) string
	Paired       func() []owners.PairedDevice
	StorageDir   string
}

// page is the data every template gets.
type page struct {
	Product    string
	Title      string
	User       *store.User
	Superadmin bool
	CSRF       string
	Data       any
}

func (d Deps) render(c fiber.Ctx, status int, name, title string, data any) error {
	u, _ := auth.UserFrom(c.Context())
	p := page{Product: d.ProductName, Title: title, User: u, Superadmin: d.Auth.IsSuperadmin(u), CSRF: d.Auth.CSRFToken(c), Data: data}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, p); err != nil {
		return err
	}
	c.Set(fiber.HeaderContentSecurityPolicy, csp)
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
		c.Set(fiber.HeaderCacheControl, "public, max-age=3600")
		c.Type(strings.TrimPrefix(extOf(name), "."))
		return c.Send(b)
	})
	mountSignin(app, d)
	mountDevices(app, d) // Task 10
	mountAccount(app, d) // Task 11
	mountAdmin(app, d)   // Task 11
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// machinePrefixes answer in JSON. Everything else is a page a person reads.
var machinePrefixes = []string{"/auth/passkey/", "/ui/devices/", "/app/", "/send/", "/user/", "/message/",
	"/chat/", "/chats", "/group/", "/newsletter/", "/devices", "/call/", "/schedule", "/mcp", "/health"}

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
