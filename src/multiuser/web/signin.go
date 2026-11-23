package web

import (
	"html"
	"html/template"
	"net/url"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/signin"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	qrcode "github.com/skip2/go-qrcode"
)

const pkCookie = "wtf_pk"

func (d Deps) waLink(c fiber.Ctx) string {
	phone := d.ServicePhone(c.Context())
	if phone == "" {
		return ""
	}
	return "https://wa.me/" + phone + "?" + url.Values{"text": {signin.Phrase + " " + d.ProductName}}.Encode()
}

// waLinkButton renders the "Continue with WhatsApp" link as trusted, pre-escaped
// HTML. html/template's attribute escaper always turns '+' into "&#43;" (a
// browser decodes it back to the same byte before using the href, but it is
// no longer the literal bytes a test - or a person reading the page source -
// would expect), so the whole anchor is built here, with html.EscapeString
// guarding the one piece that could ever hold an odd character (the phone),
// and handed to the template as already-safe HTML rather than as a plain
// string substituted into a href="" attribute.
func waLinkButton(link string) template.HTML {
	glyph, _ := icon("message-circle")
	return template.HTML(`<a class="btn btn-primary btn-lg btn-block" href="` + html.EscapeString(link) + `">` + string(glyph) + `<span>Continue with WhatsApp</span></a>`)
}

func mountSignin(app *fiber.App, d Deps) {
	app.Get("/signin", func(c fiber.Ctx) error {
		if _, ok := userOf(c); ok {
			return c.Redirect().Status(fiber.StatusSeeOther).To("/")
		}
		link := d.waLink(c)
		data := map[string]any{"WALink": link}
		if link != "" {
			data["WALinkButton"] = waLinkButton(link)
		}
		return d.render(c, fiber.StatusOK, "signin.html", "Sign in", data)
	})
	app.Get("/signin/qr.png", func(c fiber.Ctx) error {
		link := d.waLink(c)
		if link == "" {
			return fiber.ErrNotFound
		}
		png, err := qrcode.Encode(link, qrcode.Medium, 320)
		if err != nil {
			return err
		}
		c.Type("png")
		return c.Send(png)
	})
	app.Get("/signin/verify", func(c fiber.Ctx) error {
		raw := c.Query("t")
		ok, err := d.Store.LoginTokenValid(c.Context(), raw)
		if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "verify.html", "Sign in", map[string]any{
			"Token": raw, "Valid": ok, "Next": auth.SafeNext(c.Query("next")),
		})
	})
	app.Post("/signin/verify", d.Auth.RequireSameOrigin(), func(c fiber.Ctx) error {
		phone, err := d.Store.ConsumeLoginToken(c.Context(), c.FormValue("t"))
		if err != nil {
			return d.render(c, fiber.StatusOK, "verify.html", "Sign in", map[string]any{"Valid": false})
		}
		u, err := d.Store.UserByPhone(c.Context(), phone)
		if err != nil || u.Status != store.StatusActive {
			return d.render(c, fiber.StatusOK, "verify.html", "Sign in", map[string]any{"Valid": false})
		}
		if err := d.Auth.StartSession(c, u); err != nil {
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "signin", IP: c.IP()})
		return c.Redirect().Status(fiber.StatusSeeOther).To(auth.SafeNext(c.FormValue("next")))
	})
	app.Post("/signout", d.Auth.RequirePage(), d.Auth.VerifyCSRF(), func(c fiber.Ctx) error {
		if err := d.Auth.EndSession(c); err != nil {
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/signin")
	})
	app.Post("/auth/passkey/login/begin", d.Auth.RequireSameOrigin(), func(c fiber.Ctx) error {
		opts, id, err := d.Passkeys.BeginLogin(c.Context())
		if err != nil {
			return err
		}
		c.Cookie(&fiber.Cookie{Name: pkCookie, Value: id, Path: "/auth/passkey/", HTTPOnly: true, Secure: d.Auth.Secure(),
			SameSite: fiber.CookieSameSiteStrictMode, MaxAge: int((5 * time.Minute).Seconds())})
		c.Type("json")
		return c.Send(opts)
	})
	app.Post("/auth/passkey/login/finish", d.Auth.RequireSameOrigin(), func(c fiber.Ctx) error {
		u, err := d.Passkeys.FinishLogin(c.Context(), c.Cookies(pkCookie), c.Body())
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"ok": false, "message": "That passkey didn't work. Try again or continue with WhatsApp."})
		}
		if err := d.Auth.StartSession(c, u); err != nil {
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "signin_passkey", IP: c.IP()})
		return c.JSON(fiber.Map{"ok": true, "next": "/"})
	})
}
