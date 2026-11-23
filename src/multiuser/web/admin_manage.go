package web

import (
	"errors"
	"strconv"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/gofiber/fiber/v3"
)

// mountAdminManage registers the superadmin's per-user actions: unlink one
// of a user's numbers, set how many numbers they may link, and change the
// phone number they sign in with. Every route is page-gated, superadmin-only
// (404 to anyone else) and CSRF-checked, and every change is audited with
// the admin as the actor.
func mountAdminManage(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	admin := d.Auth.RequireSuperadmin()
	csrf := d.Auth.VerifyCSRF()

	app.Post("/admin/users/:id/devices/:device/unlink", page, admin, csrf, func(c fiber.Ctx) error {
		caller, _ := userOf(c)
		target, err := targetUser(c, d)
		if err != nil {
			return err
		}
		if err := d.AdminUnlinkNumber(c.Context(), caller, target.ID, c.Params("device")); err != nil {
			if errors.Is(err, pkgError.ErrDeviceNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/admin/users#user-" + target.ID)
	})

	app.Post("/admin/users/:id/limit", page, admin, csrf, func(c fiber.Ctx) error {
		caller, _ := userOf(c)
		target, err := targetUser(c, d)
		if err != nil {
			return err
		}
		n, convErr := strconv.Atoi(strings.TrimSpace(c.FormValue("max_devices")))
		if convErr != nil {
			n = -1 // refused below with the range
		}
		if err := d.AdminSetLimit(c.Context(), caller, target.ID, n, c.IP()); err != nil {
			var r *Refusal
			if errors.As(err, &r) {
				return renderUsers(c, d, r.Msg)
			}
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/admin/users#user-" + target.ID)
	})

	app.Post("/admin/users/:id/phone", page, admin, csrf, func(c fiber.Ctx) error {
		caller, _ := userOf(c)
		target, err := targetUser(c, d)
		if err != nil {
			return err
		}
		if _, err := d.AdminChangePhone(c.Context(), caller, target.ID, c.FormValue("phone"), c.IP()); err != nil {
			var r *Refusal
			if errors.As(err, &r) {
				return renderUsers(c, d, r.Msg)
			}
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/admin/users#user-" + target.ID)
	})
}

// targetUser loads the :id user, answering 404 for an unknown id.
func targetUser(c fiber.Ctx, d Deps) (*store.User, error) {
	u, err := d.Store.UserByID(c.Context(), c.Params("id"))
	if errors.Is(err, store.ErrNotFound) {
		return nil, fiber.ErrNotFound
	}
	return u, err
}
