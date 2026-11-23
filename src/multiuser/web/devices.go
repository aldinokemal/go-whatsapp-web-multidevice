package web

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/gofiber/fiber/v3"
)

// deviceLimitMessage is what a person sees at their linked-number limit; the
// owners package's own error text is for logs and the API.
const deviceLimitMessage = "You've linked as many numbers as you can. Unlink one to add another."

// mountDevices registers "/" (My WhatsApp) and its htmx fragments: creating a
// device, the QR image, the pairing-code fragment, status polling, reconnect
// and unlink. Every route runs through d.Devices, which is the caller's
// owners.ScopedDevices decorator, so a device that is not the signed-in
// user's is reported exactly like one that does not exist.
func mountDevices(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	csrf := d.Auth.VerifyCSRF()

	app.Get("/", page, func(c fiber.Ctx) error {
		list, err := d.Devices.ListDevices(c.Context())
		if err != nil {
			return err
		}
		connected := 0
		for _, dev := range list {
			if dev.PhoneNumber != "" && dev.State == domainDevice.DeviceStateLoggedIn {
				connected++
			}
		}
		// Every device row (linked or a half-linked slot) counts toward the
		// owner's limit, exactly as owners.ScopedDevices.AddDevice counts it.
		atLimit := false
		if u, ok := userOf(c); ok && u.MaxDevices > 0 {
			atLimit = len(list) >= u.MaxDevices
		}
		return d.render(c, fiber.StatusOK, "home.html", "My WhatsApp", map[string]any{
			"Devices": list, "Connected": connected, "AtLimit": atLimit, "LimitMessage": deviceLimitMessage,
		})
	})

	app.Post("/ui/devices", page, csrf, func(c fiber.Ctx) error {
		dev, err := d.Devices.AddDevice(c.Context(), "", nil)
		if err != nil {
			if errors.Is(err, owners.ErrDeviceLimitReached) {
				return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"Error": deviceLimitMessage})
			}
			var v pkgError.ValidationError
			if errors.As(err, &v) {
				return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"Error": v.Error()})
			}
			return err
		}
		return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"ID": dev.ID})
	})

	// Re-opens the shared link dialog's content for a device that was already
	// created (its slot reserved) but never finished linking, e.g. the person
	// closed the dialog before scanning. GetDevice runs the same ownership
	// check as every other route here, so this can't be used to probe
	// another user's device ids.
	app.Get("/ui/devices/:id/link", page, func(c fiber.Ctx) error {
		if _, err := d.Devices.GetDevice(c.Context(), c.Params("id")); errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		} else if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"ID": c.Params("id")})
	})

	app.Get("/ui/devices/:id/qr.png", page, func(c fiber.Ctx) error {
		resp, err := d.Devices.LoginDevice(c.Context(), c.Params("id"))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if errors.Is(err, pkgError.ErrAlreadyLoggedIn) {
			return c.SendStatus(fiber.StatusConflict)
		}
		if err != nil {
			return err
		}
		path := filepath.Clean(resp.ImagePath)
		if strings.Contains(path, "..") {
			return fiber.ErrNotFound
		}
		png, err := os.ReadFile(path)
		if err != nil {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderCacheControl, "no-store")
		c.Type("png")
		return c.Send(png)
	})

	app.Post("/ui/devices/:id/pair-code", page, csrf, func(c fiber.Ctx) error {
		code, err := d.Devices.LoginDeviceWithCode(c.Context(), c.Params("id"), c.FormValue("phone"))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{
				"ID": c.Params("id"), "Error": "We couldn't get a code for that number. Check it and try again.",
			})
		}
		return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"ID": c.Params("id"), "PairCode": code})
	})

	app.Get("/ui/devices/:id/status", page, func(c fiber.Ctx) error {
		_, loggedIn, err := d.Devices.GetStatus(c.Context(), c.Params("id"))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil || !loggedIn {
			return c.SendStatus(fiber.StatusNoContent)
		}
		c.Set("HX-Refresh", "true")
		return c.SendStatus(fiber.StatusOK)
	})

	app.Post("/ui/devices/:id/reconnect", page, csrf, func(c fiber.Ctx) error {
		if err := d.Devices.ReconnectDevice(c.Context(), c.Params("id")); err != nil {
			if errors.Is(err, pkgError.ErrDeviceNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/")
	})

	app.Post("/ui/devices/:id/unlink", page, csrf, func(c fiber.Ctx) error {
		if err := d.Devices.RemoveDevice(c.Context(), c.Params("id")); err != nil {
			if errors.Is(err, pkgError.ErrDeviceNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/")
	})
}
