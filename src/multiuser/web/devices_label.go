package web

import (
	"errors"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/gofiber/fiber/v3"
)

// MountDeviceLabelPage registers the Rename dialog (session, CSRF):
// GET /ui/devices/:id/label draws the form into the shared sheet and POST
// saves form field "label" ("" or clear=1 removes the name). For htmx a
// refused name comes back as the form with its reason and what was typed; a
// saved one closes the sheet and swaps the row in place. A plain form post is
// sent home, or answered 422 with the reason. Another user's number is 404,
// like one that does not exist.
func MountDeviceLabelPage(app *fiber.App, d Deps) {
	app.Get("/ui/devices/:id/label", d.Auth.RequirePage(), func(c fiber.Ctx) error {
		dev, err := d.Devices.GetDevice(c.Context(), c.Params("id"))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		return renderRename(c, d, dev.ID, dev.PhoneNumber, dev.Label, dev.Label != "", "")
	})
	app.Post("/ui/devices/:id/label", d.Auth.RequirePage(), d.Auth.VerifyCSRF(), func(c fiber.Ctx) error {
		id := c.Params("id")
		// Clone: fiber form values alias its request buffer, which is reused.
		typed := strings.Clone(c.FormValue("label"))
		if c.FormValue("clear") == "1" {
			typed = ""
		}
		_, err := owners.SetDeviceLabel(c.Context(), d.Store, id, typed)
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		htmx := c.Get("HX-Request") == "true"
		var v pkgError.ValidationError
		if errors.As(err, &v) {
			if !htmx {
				return c.Status(fiber.StatusUnprocessableEntity).SendString(labelErrorText(v))
			}
			dev, gerr := d.Devices.GetDevice(c.Context(), id)
			if gerr != nil {
				return gerr
			}
			return renderRename(c, d, id, dev.PhoneNumber, typed, dev.Label != "", labelErrorText(v))
		}
		if err != nil {
			return err
		}
		if !htmx {
			return c.Redirect().Status(fiber.StatusSeeOther).To("/")
		}
		data, err := deviceRowData(c, d, id, 0)
		if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "rename_done.html", "", data)
	})
}

func renderRename(c fiber.Ctx, d Deps, id, phone, label string, hasLabel bool, errMsg string) error {
	return d.render(c, fiber.StatusOK, "rename_device.html", "", map[string]any{
		"ID": id, "Phone": phone, "Label": label, "HasLabel": hasLabel, "Error": errMsg,
	})
}

// MountDeviceLabelAPI registers PUT /devices/:device_id/label, JSON
// {"label": "..."}. Mount it behind the REST API gate (session cookie or API
// key, same-origin, CSRF), as Task 8 does. No upstream route uses this path.
func MountDeviceLabelAPI(r fiber.Router, d Deps) {
	r.Put("/devices/:device_id/label", func(c fiber.Ctx) error {
		var body struct {
			Label *string `json:"label"`
		}
		if err := c.Bind().JSON(&body); err != nil || body.Label == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"status": fiber.StatusBadRequest, "code": "VALIDATION_ERROR", "message": `send {"label": "..."}; "" removes the name`,
			})
		}
		id := c.Params("device_id")
		label, err := owners.SetDeviceLabel(c.Context(), d.Store, id, *body.Label)
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"status": fiber.StatusNotFound, "code": "NOT_FOUND", "message": "device not found",
			})
		}
		var v pkgError.ValidationError
		if errors.As(err, &v) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"status": fiber.StatusBadRequest, "code": "VALIDATION_ERROR", "message": v.Error(),
			})
		}
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{
			"code": "SUCCESS", "message": "Device renamed",
			"results": fiber.Map{"device_id": id, "label": label},
		})
	})
}

// labelErrorText is what a person sees for a refused name.
func labelErrorText(v pkgError.ValidationError) string {
	if errors.Is(v, owners.ErrLabelTooLong) {
		return "Keep the name to 40 characters or fewer."
	}
	return "That name has characters we can't show. Use letters, numbers and spaces."
}
