package web

import (
	"errors"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/gofiber/fiber/v3"
)

// MountDeviceLabelPage registers POST /ui/devices/:id/label (session, CSRF):
// form field "label"; "" removes the name. Task 8 registers it with the
// pages. On success an htmx request gets HX-Refresh (Task 7 swaps the row
// instead) and a plain form post is sent home. A bad name answers 422 with
// the reason as text (Task 7 draws it in the dialog). Another user's number
// is 404, like one that does not exist.
func MountDeviceLabelPage(app *fiber.App, d Deps) {
	app.Post("/ui/devices/:id/label", d.Auth.RequirePage(), d.Auth.VerifyCSRF(), func(c fiber.Ctx) error {
		// Clone: fiber form values alias its request buffer, which is reused.
		_, err := owners.SetDeviceLabel(c.Context(), d.Store, c.Params("id"), strings.Clone(c.FormValue("label")))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		var v pkgError.ValidationError
		if errors.As(err, &v) {
			return c.Status(fiber.StatusUnprocessableEntity).SendString(labelErrorText(v))
		}
		if err != nil {
			return err
		}
		if c.Get("HX-Request") == "true" {
			c.Set("HX-Refresh", "true")
			return c.SendStatus(fiber.StatusOK)
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/")
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
