package web

import (
	"errors"
	"strings"

	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/gofiber/fiber/v3"
)

// mountWebhook registers the per-number webhook dialog. Both routes run
// through d.Devices (owners.ScopedDevices), so another user's number answers
// 404, and every URL is checked there (public https only, TLS verified) before
// it is saved. The secret is write-only: the page says whether one is set,
// never what it is.
func mountWebhook(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	csrf := d.Auth.VerifyCSRF()

	app.Get("/ui/devices/:id/webhook", page, func(c fiber.Ctx) error {
		w, err := d.NumberWebhook(c.Context(), c.Params("id"))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		return renderWebhook(c, d, c.Params("id"), w, "", "")
	})

	app.Post("/ui/devices/:id/webhook", page, csrf, func(c fiber.Ctx) error {
		id := c.Params("id")
		remove := c.FormValue("remove") == "1"
		// Clone: fiber form values alias its request buffer, which is reused.
		url := strings.Clone(strings.TrimSpace(c.FormValue("url")))
		saved, err := d.SaveNumberWebhook(c.Context(), id, url, strings.Clone(c.FormValue("secret")), remove, c.IP())
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		var r *Refusal
		if errors.As(err, &r) {
			// The form again: what was typed, and whether a secret is set now.
			shown, gerr := d.NumberWebhook(c.Context(), id)
			if gerr != nil {
				return gerr
			}
			if !remove && url != "" {
				shown.URL = url
			}
			return renderWebhook(c, d, id, shown, r.Msg, "")
		}
		if err != nil {
			return err
		}
		msg := "Saved."
		if remove {
			msg = "Webhook removed."
		}
		return renderWebhook(c, d, id, saved, "", msg)
	})
}

// webhookErrorText turns a validation error into a sentence for the dialog.
func webhookErrorText(msg string) string {
	if msg == "" {
		return "That address can't be used."
	}
	return strings.ToUpper(msg[:1]) + msg[1:] + "."
}

func renderWebhook(c fiber.Ctx, d Deps, id string, w WebhookInfo, errMsg, saved string) error {
	return d.render(c, fiber.StatusOK, "webhook_form.html", "", map[string]any{
		"ID": id, "URL": w.URL, "HasSecret": w.HasSecret, "Error": errMsg, "Saved": saved,
	})
}
