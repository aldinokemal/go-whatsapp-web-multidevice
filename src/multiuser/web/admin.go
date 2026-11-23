package web

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"path/filepath"
	"runtime"
	"time"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
)

const signinLinkTTL = 15 * time.Minute

// mountAdmin registers "/admin/users" and "/admin/settings" behind the page
// gate and the superadmin gate. Task 9 already installed those gates (so an
// anonymous or non-superadmin visitor is refused correctly from that task
// on); this task replaces the placeholder body with the real screens.
func mountAdmin(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	admin := d.Auth.RequireSuperadmin()
	csrf := d.Auth.VerifyCSRF()

	app.Get("/admin/users", page, admin, func(c fiber.Ctx) error {
		return renderUsers(c, d, "")
	})

	// Add user: the only way to create an account from the UI, for when
	// sign-ups are closed. Answers the htmx modal with either the form again
	// (with the error and what was typed) or the new user's sign-in link.
	app.Post("/admin/users", page, admin, csrf, func(c fiber.Ctx) error {
		typed := c.FormValue("phone")
		phone, err := store.NormalizePhone(typed)
		if err != nil {
			return d.render(c, fiber.StatusOK, "add_user.html", "", map[string]any{
				"Phone": typed, "Error": "Enter a full number with its country code, like +27 82 555 0100.",
			})
		}
		if _, err := d.Store.UserByPhone(c.Context(), phone); err == nil {
			return d.render(c, fiber.StatusOK, "add_user.html", "", map[string]any{
				"Phone": typed, "Error": "That number already has an account. Use Get sign-in link on its row instead.",
			})
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		u, err := d.Store.CreateUser(c.Context(), phone)
		if err != nil {
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "user_added", IP: c.IP()})
		link, err := signinLinkFor(c, d, phone)
		if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "add_user.html", "", map[string]any{"Link": link, "Phone": phone})
	})

	app.Post("/admin/users/:id/status", page, admin, csrf, func(c fiber.Ctx) error {
		caller, _ := userOf(c)
		targetID := c.Params("id")
		status := c.FormValue("status")
		if status != store.StatusActive && status != store.StatusDisabled {
			return renderUsers(c, d, "That's not a status we know.")
		}
		if status == store.StatusDisabled && targetID == caller.ID {
			return renderUsers(c, d, "You can't disable your own account.")
		}
		if err := d.Store.SetUserStatus(c.Context(), targetID, status); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		if status == store.StatusDisabled {
			if err := d.Store.DeleteUserSessions(c.Context(), targetID); err != nil {
				return err
			}
			_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: targetID, Action: "user_disabled", IP: c.IP()})
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/admin/users")
	})

	app.Post("/admin/users/:id/signin-link", page, admin, csrf, func(c fiber.Ctx) error {
		target, err := d.Store.UserByID(c.Context(), c.Params("id"))
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		if target.Status != store.StatusActive {
			return d.render(c, fiber.StatusOK, "signin_link.html", "", map[string]any{
				"Error": "Enable this account first, then get a sign-in link.",
			})
		}
		link, err := signinLinkFor(c, d, target.Phone)
		if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "signin_link.html", "", map[string]any{"Link": link})
	})

	app.Get("/admin/settings", page, admin, func(c fiber.Ctx) error {
		return renderSettings(c, d, "")
	})

	app.Post("/admin/settings/service-number", page, admin, csrf, func(c fiber.Ctx) error {
		caller, _ := userOf(c)
		deviceID := c.FormValue("device_id")
		ids, err := d.Store.DevicesOf(c.Context(), caller.ID)
		if err != nil {
			return err
		}
		allowed := false
		for _, id := range ids {
			if id == deviceID {
				allowed = true
				break
			}
		}
		if !allowed {
			return renderSettings(c, d, "Pick one of your own linked numbers.")
		}
		if err := d.Store.SetSetting(c.Context(), store.SettingServiceDeviceID, deviceID, caller.ID); err != nil {
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: caller.ID, Action: "service_number_set", DeviceID: deviceID, IP: c.IP()})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/admin/settings")
	})
}

// signinLinkFor mints a one-time sign-in link for phone, the same link the
// "Get sign-in link" button and the admin-link command hand out.
func signinLinkFor(c fiber.Ctx, d Deps, phone string) (string, error) {
	raw, err := d.Store.CreateLoginToken(c.Context(), phone, signinLinkTTL)
	if err != nil {
		return "", err
	}
	return d.PublicURL + "/signin/verify?t=" + url.QueryEscape(raw), nil
}

// userRow is what admin_users.html renders: one line per account, with the
// relative time and the "is this me" / "how many devices" facts already
// resolved so the template holds no logic beyond which pill or button to show.
type userRow struct {
	ID          string
	Phone       string
	Active      bool
	IsSelf      bool
	DeviceCount int
	LastSignIn  string
	JustAdded   bool // created in the last ten minutes and never signed in
}

func userRows(ctx context.Context, st *store.Store, users []store.User, callerID string) ([]userRow, error) {
	rows := make([]userRow, 0, len(users))
	for _, u := range users {
		ids, err := st.DevicesOf(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		rows = append(rows, userRow{
			ID: u.ID, Phone: u.Phone, Active: u.Status == store.StatusActive,
			IsSelf: u.ID == callerID, DeviceCount: len(ids), LastSignIn: humanAgo(u.LastLoginAt),
			JustAdded: u.LastLoginAt == nil && time.Since(u.CreatedAt) < 10*time.Minute,
		})
	}
	return rows, nil
}

func usersPageData(c fiber.Ctx, d Deps, errMsg string) (map[string]any, error) {
	users, err := d.Store.ListUsers(c.Context())
	if err != nil {
		return nil, err
	}
	caller, _ := userOf(c)
	callerID := ""
	if caller != nil {
		callerID = caller.ID
	}
	rows, err := userRows(c.Context(), d.Store, users, callerID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"Rows": rows, "Error": errMsg}, nil
}

func renderUsers(c fiber.Ctx, d Deps, errMsg string) error {
	data, err := usersPageData(c, d, errMsg)
	if err != nil {
		return err
	}
	status := fiber.StatusOK
	if errMsg != "" {
		status = fiber.StatusBadRequest
	}
	return d.render(c, status, "admin_users.html", "Users", data)
}

// diskUsage sums file sizes under dir. It is best-effort: an unreadable entry
// is skipped rather than failing the whole settings page, since this number
// is informational, not load-bearing.
func diskUsage(dir string) int64 {
	if dir == "" {
		return 0
	}
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		if info, err := de.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

const bytesPerMB = 1 << 20

func settingsPageData(c fiber.Ctx, d Deps, errMsg string) (map[string]any, error) {
	ctx := c.Context()
	devices, err := d.Devices.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	linked := make([]domainDevice.Device, 0, len(devices))
	for _, dev := range devices {
		if dev.PhoneNumber != "" {
			linked = append(linked, dev)
		}
	}
	serviceDeviceID, _, err := d.Store.Setting(ctx, store.SettingServiceDeviceID)
	if err != nil {
		return nil, err
	}
	total, err := d.Store.OwnedDeviceCount(ctx)
	if err != nil {
		return nil, err
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return map[string]any{
		"Devices":         linked,
		"ServiceDeviceID": serviceDeviceID,
		"ServiceStatus":   serviceStatus(c, d, serviceDeviceID),
		"LinkedTotal":     total,
		"AllocMB":         mem.Alloc / bytesPerMB,
		"SysMB":           mem.Sys / bytesPerMB,
		"Goroutines":      runtime.NumGoroutine(),
		"DiskMB":          diskUsage(d.StorageDir) / bytesPerMB,
		"Error":           errMsg,
	}, nil
}

func renderSettings(c fiber.Ctx, d Deps, errMsg string) error {
	data, err := settingsPageData(c, d, errMsg)
	if err != nil {
		return err
	}
	status := fiber.StatusOK
	if errMsg != "" {
		status = fiber.StatusBadRequest
	}
	return d.render(c, status, "admin_settings.html", "Settings", data)
}

// serviceStatus says whether sign-in by WhatsApp works: "Not set", or the
// saved number's live state ("Connected" / "Not connected").
func serviceStatus(c fiber.Ctx, d Deps, id string) string {
	switch {
	case id == "":
		return "Not set"
	case d.ServicePhone != nil && d.ServicePhone(c.Context()) != "":
		return "Connected"
	default:
		return "Not connected"
	}
}
