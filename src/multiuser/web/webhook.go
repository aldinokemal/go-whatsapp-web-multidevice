package web

import (
	"errors"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
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
		cfg, err := d.Devices.GetDeviceWebhookConfig(c.Context(), c.Params("id"))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		return renderWebhook(c, d, c.Params("id"), cfg, "", "")
	})

	app.Post("/ui/devices/:id/webhook", page, csrf, func(c fiber.Ctx) error {
		id := c.Params("id")
		cur, err := d.Devices.GetDeviceWebhookConfig(c.Context(), id)
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur == nil {
			cur = &chatstorage.DeviceWebhookConfig{}
		}
		next := &chatstorage.DeviceWebhookConfig{WebhookEvents: cur.WebhookEvents, WebhookSecret: cur.WebhookSecret}
		action := "webhook_set"
		if c.FormValue("remove") == "1" {
			empty := ""
			next.WebhookURL, next.WebhookSecret, action = &empty, "", "webhook_cleared"
		} else {
			// Clone: fiber form values alias its request buffer, which is reused.
			url := strings.Clone(strings.TrimSpace(c.FormValue("url")))
			if url == "" {
				return renderWebhook(c, d, id, cur, "Enter the address to send events to.", "")
			}
			next.WebhookURL = &url
			if s := c.FormValue("secret"); s != "" {
				next.WebhookSecret = strings.Clone(s)
			}
		}
		if err := d.Devices.SetDeviceWebhookConfig(c.Context(), id, next); err != nil {
			var v pkgError.ValidationError
			if errors.As(err, &v) {
				shown := next
				if action == "webhook_set" {
					shown = &chatstorage.DeviceWebhookConfig{WebhookURL: next.WebhookURL, WebhookSecret: cur.WebhookSecret}
				}
				return renderWebhook(c, d, id, shown, webhookErrorText(v.Error()), "")
			}
			return err
		}
		u, _ := userOf(c)
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: action, DeviceID: id, IP: c.IP()})
		saved := "Saved."
		if action == "webhook_cleared" {
			saved = "Webhook removed."
		}
		return renderWebhook(c, d, id, next, "", saved)
	})
}

// webhookErrorText turns a validation error into a sentence for the dialog.
func webhookErrorText(msg string) string {
	if msg == "" {
		return "That address can't be used."
	}
	return strings.ToUpper(msg[:1]) + msg[1:] + "."
}

func renderWebhook(c fiber.Ctx, d Deps, id string, cfg *chatstorage.DeviceWebhookConfig, errMsg, saved string) error {
	url, hasSecret := "", false
	if cfg != nil {
		if cfg.WebhookURL != nil {
			url = *cfg.WebhookURL
		}
		hasSecret = cfg.WebhookSecret != ""
	}
	return d.render(c, fiber.StatusOK, "webhook_form.html", "", map[string]any{
		"ID": id, "URL": url, "HasSecret": hasSecret, "Error": errMsg, "Saved": saved,
	})
}
