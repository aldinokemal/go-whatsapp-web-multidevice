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
		return c.Redirect().Status(fiber.StatusSeeOther).To(userRowURL(target))
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
		return c.Redirect().Status(fiber.StatusSeeOther).To(userRowURL(target))
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
		return c.Redirect().Status(fiber.StatusSeeOther).To(userRowURL(target))
	})
}

// mountAdminDelete registers POST /admin/users/:id/delete: a superadmin
// deletes a disabled account (never their own, never the sign-in number's
// owner) with the same Deleter as Delete my account, after typing its
// number. Page-gated, superadmin-only (404 to anyone else), CSRF-checked and
// audited with the admin as the actor. The guards live in
// Deps.AdminDeleteUser, shared with the MCP tool. The dialog posts with
// htmx: an error comes back as its form again, success as HX-Redirect to
// Users.
func mountAdminDelete(app *fiber.App, d Deps) {
	app.Post("/admin/users/:id/delete", d.Auth.RequirePage(), d.Auth.RequireSuperadmin(), d.Auth.VerifyCSRF(), func(c fiber.Ctx) error {
		if d.DeleteAccount == nil {
			return fiber.ErrNotFound
		}
		caller, _ := userOf(c)
		target, err := targetUser(c, d)
		if err != nil {
			return err
		}
		ctx := c.Context()
		err = d.AdminDeleteUser(ctx, caller, target.ID, c.FormValue("confirm"), c.IP())
		var r *Refusal
		if errors.As(err, &r) {
			v, verr := deleteAccountData(ctx, d, target)
			if verr != nil {
				return verr
			}
			v.Their, v.UserID, v.Phone = true, target.ID, target.Phone
			msg, link := r.Msg, errors.Is(err, ErrOwnsSigninNumber)
			if link {
				msg = ""
			}
			v.Error, v.ErrorLink, v.Invalid = msg, link, msg == typeTheirToConfirm
			if c.Get("HX-Request") != "true" {
				if link {
					msg = "This account has the sign-in number. Choose another one in Settings."
				}
				return renderUsers(c, d, msg)
			}
			return d.render(c, fiber.StatusOK, "admin_delete_form.html", "", v)
		}
		if err != nil {
			return err
		}
		if c.Get("HX-Request") == "true" {
			c.Set("HX-Redirect", "/admin/users")
			return c.SendStatus(fiber.StatusOK)
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/admin/users")
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
