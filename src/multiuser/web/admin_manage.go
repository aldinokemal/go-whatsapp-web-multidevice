package web

import (
	"errors"
	"strconv"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
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
		if err := owners.RemoveDeviceAsAdmin(c.Context(), d.Devices, caller, target, c.Params("device")); err != nil {
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
		if convErr != nil || n < 0 || n > store.MaxDevicesLimit {
			return renderUsers(c, d, "Choose a number from 0 to 20.")
		}
		if err := d.Store.SetMaxDevices(c.Context(), target.ID, n); err != nil {
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: target.ID, ActorID: caller.ID, Action: "user_limit_set",
			Detail: strconv.Itoa(n), IP: c.IP()})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/admin/users#user-" + target.ID)
	})

	app.Post("/admin/users/:id/phone", page, admin, csrf, func(c fiber.Ctx) error {
		caller, _ := userOf(c)
		target, err := targetUser(c, d)
		if err != nil {
			return err
		}
		if target.ID == caller.ID {
			return renderUsers(c, d, "You can't change your own number here.")
		}
		// Superadmin is decided by phone number, so moving an account onto
		// or off such a number would grant or remove admin rights.
		if d.Auth.IsSuperadminPhone(target.Phone) {
			return renderUsers(c, d, "An admin's number is set in the server's settings.")
		}
		phone, err := store.NormalizePhone(c.FormValue("phone"))
		if err != nil {
			return renderUsers(c, d, "Enter the full number with its country code.")
		}
		if d.Auth.IsSuperadminPhone(phone) {
			return renderUsers(c, d, "That number belongs to an admin.")
		}
		if phone == target.Phone {
			return c.Redirect().Status(fiber.StatusSeeOther).To("/admin/users#user-" + target.ID)
		}
		old, err := d.Store.SetUserPhone(c.Context(), target.ID, phone)
		if errors.Is(err, store.ErrPhoneTaken) {
			return renderUsers(c, d, "That number already belongs to another user.")
		}
		if err != nil {
			return err
		}
		if err := d.Store.DeleteUserSessions(c.Context(), target.ID); err != nil {
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: target.ID, ActorID: caller.ID, Action: "user_phone_changed",
			Detail: old + " " + phone, IP: c.IP()})
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
