package web

import "github.com/gofiber/fiber/v3"

// mountAdmin registers "/admin/users" and "/admin/settings" behind the page
// gate and the superadmin gate. Task 11 replaces the placeholder body with
// the real screens; the gates themselves belong here so an anonymous or
// non-superadmin visitor is already refused correctly from this task on.
func mountAdmin(app *fiber.App, d Deps) {
	placeholder := func(c fiber.Ctx) error {
		return d.render(c, fiber.StatusOK, "error.html", "Admin", map[string]any{
			"Message": "This page isn't built yet.", "Code": fiber.StatusOK,
		})
	}
	app.Get("/admin/users", d.Auth.RequirePage(), d.Auth.RequireSuperadmin(), placeholder)
	app.Get("/admin/settings", d.Auth.RequirePage(), d.Auth.RequireSuperadmin(), placeholder)
}
