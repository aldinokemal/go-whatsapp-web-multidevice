package web

import (
	"errors"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
)

// apiKeyView is one key as the Account page lists it: never the key itself,
// only its name and the few characters that tell keys apart.
type apiKeyView struct {
	ID       string
	Name     string
	Shown    string // "wtfk_AbC123…"
	Added    string
	LastUsed string // "never" when unused
}

func apiKeyViews(list []store.APIKey) []apiKeyView {
	out := make([]apiKeyView, 0, len(list))
	for _, k := range list {
		created := k.CreatedAt
		out = append(out, apiKeyView{
			ID: k.ID, Name: k.Name, Shown: store.APIKeyPrefix + k.Prefix + "…",
			Added: humanAgo(&created), LastUsed: humanAgo(k.LastUsedAt),
		})
	}
	return out
}

// apiKeysData is what account.html needs for the API keys card.
func apiKeysData(c fiber.Ctx, d Deps, userID string) (map[string]any, error) {
	list, err := d.Store.APIKeysOf(c.Context(), userID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Keys":    apiKeyViews(list),
		"Max":     store.MaxAPIKeysPerUser,
		"AtLimit": len(list) >= store.MaxAPIKeysPerUser,
	}, nil
}

// mountAPIKeys registers the personal API key actions on the Account page.
// They are page routes: a session cookie and its CSRF token only. An API
// key never reaches them (the key loader sits on the REST API gate, after
// these routes), so a key cannot mint or revoke keys. Every action works on
// the signed-in user's own keys; another user's key id is a 404.
func mountAPIKeys(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	csrf := d.Auth.VerifyCSRF()

	app.Post("/account/api-keys", page, csrf, func(c fiber.Ctx) error {
		u, _ := userOf(c)
		name := strings.Clone(c.FormValue("name")) // fiber form values alias a reused buffer
		raw, k, err := d.Store.CreateAPIKey(c.Context(), u.ID, name)
		if err != nil {
			msg := ""
			switch {
			case errors.Is(err, store.ErrInvalid):
				msg = "Give the key a name of up to 60 characters."
			case errors.Is(err, store.ErrTooManyKeys):
				msg = "You already have 10 keys. Revoke one to make another."
			default:
				return err
			}
			return d.render(c, fiber.StatusOK, "new_key.html", "", map[string]any{"Name": name, "Error": msg})
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "api_key_created", IP: c.IP()})
		return d.render(c, fiber.StatusOK, "new_key.html", "", map[string]any{"Key": raw, "Name": k.Name})
	})

	app.Post("/account/api-keys/:id/revoke", page, csrf, func(c fiber.Ctx) error {
		u, _ := userOf(c)
		if err := d.Store.RevokeAPIKey(c.Context(), u.ID, c.Params("id")); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "api_key_revoked", IP: c.IP()})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/account")
	})
}
