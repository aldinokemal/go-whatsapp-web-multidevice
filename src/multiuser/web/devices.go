package web

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
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
		data, err := homeData(c, d)
		if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "home.html", "My WhatsApp", data)
	})

	// One device row, re-drawn in place while a reconnect settles (htmx).
	// n counts the checks so far; after reconnectChecks it gives up and says so.
	app.Get("/ui/devices/:id/row", page, func(c fiber.Ctx) error {
		n, _ := strconv.Atoi(c.Query("n"))
		return renderRow(c, d, c.Params("id"), n)
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
		mode := c.Query("mode") // "qr" or "pair" pins the link dialog's view
		if mode != "qr" && mode != "pair" {
			mode = ""
		}
		return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"ID": c.Params("id"), "Mode": mode})
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

	// Reconnect answers htmx with the row itself (reconnecting, then
	// connected or a way to link again); a plain form post redirects home.
	// A failure is never a full-page error.
	app.Post("/ui/devices/:id/reconnect", page, csrf, func(c fiber.Ctx) error {
		id := c.Params("id")
		err := d.Devices.ReconnectDevice(c.Context(), id)
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if c.Get("HX-Request") != "true" {
			if err != nil {
				logrus.WithError(err).Warnf("[WTF] reconnect %s", id)
			}
			return c.Redirect().Status(fiber.StatusSeeOther).To("/")
		}
		if err != nil {
			logrus.WithError(err).Warnf("[WTF] reconnect %s", id)
			return renderRow(c, d, id, reconnectChecks)
		}
		return renderRow(c, d, id, 1)
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

// reconnectChecks is how many times (3 seconds apart) a reconnecting row
// checks in before it says the reconnect did not work.
const reconnectChecks = 5

// homeData is everything "/" shows, derived from real state at request time.
func homeData(c fiber.Ctx, d Deps) (map[string]any, error) {
	ctx := c.Context()
	list, err := d.Devices.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	u, _ := userOf(c)
	connected := 0
	for _, dev := range list {
		if dev.PhoneNumber != "" && dev.State == domainDevice.DeviceStateLoggedIn {
			connected++
		}
	}
	// Every device row (linked or a half-linked slot) counts toward the
	// owner's limit, exactly as owners.ScopedDevices.AddDevice counts it.
	atLimit := u != nil && u.MaxDevices > 0 && len(list) >= u.MaxDevices
	data := map[string]any{
		"Devices": list, "Connected": connected, "AtLimit": atLimit, "LimitMessage": deviceLimitMessage, "ServiceID": "",
	}
	if u != nil {
		// What each slot remembers (signed out from the phone, refused link).
		slots, err := d.Store.SlotsOf(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		data["Numbers"] = numberMap(list, slots)
		pks, err := d.Store.PasskeysOf(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		data["NoPasskeys"] = len(pks) == 0
	}
	if err := addServiceState(c, d, data); err != nil {
		return nil, err
	}
	return data, nil
}

// addServiceState adds, for a superadmin only, the sign-in (service) number
// state: which device it is, and whether sign-in is down because that number
// is set but not connected.
func addServiceState(c fiber.Ctx, d Deps, data map[string]any) error {
	u, _ := userOf(c)
	if !d.Auth.IsSuperadmin(u) {
		return nil
	}
	id, _, err := d.Store.Setting(c.Context(), store.SettingServiceDeviceID)
	if err != nil {
		return err
	}
	data["IsAdmin"] = true
	data["ServiceID"] = id
	data["ServiceDown"] = id != "" && d.ServicePhone != nil && d.ServicePhone(c.Context()) == ""
	return nil
}

// renderRow draws one device row for htmx. checks < reconnectChecks while the
// device is not yet connected keeps it checking; at the limit it gives up.
func renderRow(c fiber.Ctx, d Deps, id string, checks int) error {
	data, err := deviceRowData(c, d, id, checks)
	if err != nil {
		return err
	}
	return d.render(c, fiber.StatusOK, "device_row.html", "", data)
}

// deviceRowData is what one device row shows, for renderRow and Rename.
func deviceRowData(c fiber.Ctx, d Deps, id string, checks int) (map[string]any, error) {
	list, err := d.Devices.ListDevices(c.Context())
	if err != nil {
		return nil, err
	}
	var dev *domainDevice.Device
	for i := range list {
		if list[i].ID == id {
			dev = &list[i]
		}
	}
	if dev == nil {
		return nil, fiber.ErrNotFound
	}
	data := map[string]any{"D": *dev, "ServiceID": ""}
	if u, ok := userOf(c); ok {
		slots, err := d.Store.SlotsOf(c.Context(), u.ID)
		if err != nil {
			return nil, err
		}
		data["N"] = numberMap([]domainDevice.Device{*dev}, slots)[dev.ID]
	}
	switch {
	case dev.State == domainDevice.DeviceStateLoggedIn && checks > 0:
		data["Flash"] = "Reconnected."
	case dev.State == domainDevice.DeviceStateLoggedIn:
	case checks >= reconnectChecks:
		data["Failed"] = true
	default:
		data["Checking"] = checks + 1
	}
	if err := addServiceState(c, d, data); err != nil {
		return nil, err
	}
	return data, nil
}
