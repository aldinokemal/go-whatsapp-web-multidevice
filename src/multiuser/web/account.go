package web

import (
	"errors"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/passkey"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
)

// pkRegCookie carries the registration ceremony id between
// /auth/passkey/register/begin and /auth/passkey/register/finish. It is
// path-scoped to /auth/passkey/ like the sign-in ceremony's wtf_pk cookie,
// so the two ceremonies never collide even though they share a name.
const pkRegCookie = "wtf_pk"

// deviceName turns a User-Agent into a short label for a passkey, e.g.
// "Safari on iOS". Edge is checked before Chrome since Edge's UA string
// contains "Chrome/" too. An unrecognised UA falls back to "This device".
func deviceName(ua string) string {
	var browser string
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Safari/") && strings.Contains(ua, "Version/"):
		browser = "Safari"
	}
	var os string
	switch {
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") || strings.Contains(ua, "iOS"):
		os = "iOS"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		os = "macOS"
	case strings.Contains(ua, "Windows"):
		os = "Windows"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}
	if browser == "" {
		return "This device"
	}
	if os == "" {
		return browser
	}
	return browser + " on " + os
}

// passkeyView is what account.html renders: the clock-free, formatted shape
// of a store.Passkey. Formatting relative times in Go rather than in the
// template keeps humanAgo's *time.Time signature (built for the pointer
// fields Task 9/10 already have) out of the template language entirely.
type passkeyView struct {
	CredentialID string
	Name         string
	Added        string
	LastUsed     string
}

func passkeyViews(list []store.Passkey) []passkeyView {
	out := make([]passkeyView, 0, len(list))
	for _, p := range list {
		created := p.CreatedAt
		out = append(out, passkeyView{
			CredentialID: p.CredentialID,
			Name:         p.Name,
			Added:        humanAgo(&created),
			LastUsed:     humanAgo(p.LastUsedAt),
		})
	}
	return out
}

// passkeyErrorMessage turns a passkey package error into words safe to show
// in the browser: never the ceremony, ids or verification detail.
func passkeyErrorMessage(err error) string {
	switch {
	case errors.Is(err, passkey.ErrTooMany):
		return "You already have the maximum number of passkeys."
	case errors.Is(err, passkey.ErrDisabled):
		return "This account is disabled."
	case errors.Is(err, passkey.ErrBusy):
		return "Too many requests right now. Try again shortly."
	case errors.Is(err, passkey.ErrCeremony):
		return "That took too long. Try adding the passkey again."
	default:
		return "That didn't work. Try again."
	}
}

func passkeyFailJSON(c fiber.Ctx, err error) error {
	return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"ok": false, "message": passkeyErrorMessage(err)})
}

// mountAccount registers "/account" (the signed-in user's own passkeys and
// sessions) and the passkey registration JSON endpoints. Every route here
// acts only on the signed-in user: there is no :id in any path, so there is
// nothing to ownership-check beyond "is somebody signed in".
func mountAccount(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	csrf := d.Auth.VerifyCSRF()

	app.Get("/account", page, func(c fiber.Ctx) error {
		u, _ := userOf(c)
		list, err := d.Store.PasskeysOf(c.Context(), u.ID)
		if err != nil {
			return err
		}
		sessions, err := sessionViews(c, d, u.ID)
		if err != nil {
			return err
		}
		keys, err := apiKeysData(c, d, u.ID)
		if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "account.html", "Account", map[string]any{"Passkeys": passkeyViews(list), "Sessions": sessions, "APIKeys": keys})
	})
	mountAPIKeys(app, d)

	app.Post("/auth/passkey/register/begin", page, csrf, func(c fiber.Ctx) error {
		u, _ := userOf(c)
		opts, id, err := d.Passkeys.BeginRegistration(c.Context(), u)
		if err != nil {
			return passkeyFailJSON(c, err)
		}
		c.Cookie(&fiber.Cookie{Name: pkRegCookie, Value: id, Path: "/auth/passkey/", HTTPOnly: true, Secure: d.Auth.Secure(),
			SameSite: fiber.CookieSameSiteStrictMode, MaxAge: int((5 * time.Minute).Seconds())})
		c.Type("json")
		return c.Send(opts)
	})

	app.Post("/auth/passkey/register/finish", page, csrf, func(c fiber.Ctx) error {
		u, _ := userOf(c)
		name := deviceName(c.Get(fiber.HeaderUserAgent))
		if err := d.Passkeys.FinishRegistration(c.Context(), u, c.Cookies(pkRegCookie), c.Body(), name); err != nil {
			return passkeyFailJSON(c, err)
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	app.Post("/account/passkeys/:cred/delete", page, csrf, func(c fiber.Ctx) error {
		u, _ := userOf(c)
		if err := d.Store.DeletePasskey(c.Context(), u.ID, c.Params("cred")); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/account")
	})

	app.Post("/account/signout-everywhere", page, csrf, func(c fiber.Ctx) error {
		u, _ := userOf(c)
		if err := d.Store.DeleteUserSessions(c.Context(), u.ID); err != nil {
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "sessions_ended_all", IP: c.IP()})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/signin")
	})
}
