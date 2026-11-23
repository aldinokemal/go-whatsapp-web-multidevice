package web

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/capacity"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
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
		var u *store.User
		if d.DefaultMaxDevices > 0 {
			u, err = d.Store.CreateUserWithLimit(c.Context(), phone, d.DefaultMaxDevices)
		} else {
			u, err = d.Store.CreateUser(c.Context(), phone)
		}
		if err != nil {
			return err
		}
		caller, _ := userOf(c)
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, ActorID: caller.ID, Action: "user_added", IP: c.IP()})
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
			if errors.Is(err, store.ErrDeleting) {
				return renderUsers(c, d, "This account is being deleted.")
			}
			if errors.Is(err, store.ErrNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		if status == store.StatusDisabled {
			if err := revokeCredentials(c.Context(), d, targetID); err != nil {
				return err
			}
			_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: targetID, ActorID: caller.ID, Action: "user_disabled", IP: c.IP()})
			// The confirm dialog's "Also unlink their numbers" box (on by
			// default). An unticked box sends nothing, which keeps them.
			if c.FormValue("unlink") == "on" {
				target, err := d.Store.UserByID(c.Context(), targetID)
				if err != nil {
					return err
				}
				if _, err := owners.RemoveAllDevicesAsAdmin(c.Context(), d.Devices, d.Store, caller, target); err != nil {
					return err
				}
			}
		} else {
			_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: targetID, ActorID: caller.ID, Action: "user_enabled", IP: c.IP()})
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

// revokeCredentials ends everything that lets userID in without signing in
// again: sessions, API keys and connected apps (OAuth grants). Disabling an
// account and changing its number both call it, so enabling the account
// again, or the old number, never brings an old credential back.
func revokeCredentials(ctx context.Context, d Deps, userID string) error {
	if err := d.Store.DeleteUserSessions(ctx, userID); err != nil {
		return err
	}
	if err := d.Store.RevokeAllAPIKeys(ctx, userID); err != nil {
		return err
	}
	if d.Grants != nil {
		if err := d.Grants.RevokeSubject(ctx, userID); err != nil {
			return err
		}
	}
	return nil
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
	IsAdmin     bool // a superadmin: their number is fixed by the server's settings
	DeviceCount int
	MaxDevices  int
	LastSignIn  string
	Added       time.Time
	Numbers     []numberView
	JustAdded   bool // created in the last ten minutes and never signed in
	// Delete is the Delete account dialog, for a disabled user other than
	// the caller who does not hold the sign-in number; nil hides it.
	Delete *deleteView
}

func userRows(ctx context.Context, d Deps, users []store.User, caller *store.User) ([]userRow, error) {
	rows := make([]userRow, 0, len(users))
	for i := range users {
		u := &users[i]
		row := userRow{
			ID: u.ID, Phone: u.Phone, Active: u.Status == store.StatusActive, IsSelf: caller != nil && u.ID == caller.ID,
			IsAdmin: d.Auth.IsSuperadminPhone(u.Phone), MaxDevices: u.MaxDevices, LastSignIn: humanAgo(u.LastLoginAt), Added: u.CreatedAt,
			JustAdded: u.LastLoginAt == nil && time.Since(u.CreatedAt) < 10*time.Minute,
		}
		if d.Devices != nil && caller != nil {
			devs, err := owners.ListDevicesAsAdmin(ctx, d.Devices, caller, u)
			if err != nil {
				return nil, err
			}
			slots, err := d.Store.SlotsOf(ctx, u.ID)
			if err != nil {
				return nil, err
			}
			row.Numbers = numberViews(devs, slots)
			row.DeviceCount = len(devs)
		} else {
			ids, err := d.Store.DevicesOf(ctx, u.ID)
			if err != nil {
				return nil, err
			}
			row.DeviceCount = len(ids)
		}
		if !row.Active && !row.IsSelf && u.Status == store.StatusDisabled {
			v, err := deleteAccountData(ctx, d, u)
			if err != nil {
				return nil, err
			}
			if v != nil && !v.OwnsService {
				v.Their, v.UserID, v.Phone = true, u.ID, u.Phone
				row.Delete = v
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// usersQuery is the Users list's search, filter, sort and page, read from
// the URL and written back into every link so they survive each other.
type usersQuery struct {
	Q      string // as typed; the store keeps its digits
	Status string // "", active, disabled
	Sort   string // "", created, last_login, phone (the store's allowlist)
	Desc   bool
	Page   int
}

func parseUsersQuery(c fiber.Ctx) usersQuery {
	q := usersQuery{Q: strings.TrimSpace(c.Query("q")), Page: 1}
	if st := c.Query("status"); st == store.StatusActive || st == store.StatusDisabled {
		q.Status = st
	}
	switch c.Query("sort") {
	case "created", "last_login", "phone":
		q.Sort = c.Query("sort")
		q.Desc = c.Query("dir") == "desc"
	}
	if n, err := strconv.Atoi(c.Query("page")); err == nil && n > 1 {
		q.Page = n
	}
	return q
}

// URL is the list with these choices, in a fixed order, page 1 left out.
func (q usersQuery) URL() string {
	v := []string{}
	add := func(k, val string) {
		if val != "" {
			v = append(v, k+"="+url.QueryEscape(val))
		}
	}
	add("q", q.Q)
	add("status", q.Status)
	add("sort", q.Sort)
	if q.Sort != "" && q.Desc {
		add("dir", "desc")
	}
	if q.Page > 1 {
		add("page", strconv.Itoa(q.Page))
	}
	if len(v) == 0 {
		return "/admin/users"
	}
	return "/admin/users?" + strings.Join(v, "&")
}

// sortHeader is one sortable column header: where it links and how the list
// is sorted by it now ("", ascending, descending).
type sortHeader struct {
	Label, Href, AriaSort string
}

func (q usersQuery) header(label, key string) sortHeader {
	cur, desc := q.Sort, q.Desc
	if cur == "" { // the default: newest first
		cur, desc = "created", true
	}
	h := sortHeader{Label: label}
	next := q
	next.Page = 1
	next.Sort, next.Desc = key, false
	if cur == key {
		h.AriaSort = "ascending"
		if desc {
			h.AriaSort = "descending"
		} else {
			next.Desc = true
		}
	}
	h.Href = next.URL()
	return h
}

func usersPageData(c fiber.Ctx, d Deps, errMsg string) (map[string]any, error) {
	q := parseUsersQuery(c)
	total, users, err := d.Store.ListUsersPage(c.Context(), store.UsersQuery{Search: q.Q, Status: q.Status, Sort: q.Sort,
		Desc: q.Desc, Offset: (q.Page - 1) * listPageSize, Limit: listPageSize})
	if err != nil {
		return nil, err
	}
	caller, _ := userOf(c)
	rows, err := userRows(c.Context(), d, users, caller)
	if err != nil {
		return nil, err
	}
	pg := pager{Total: total}
	if len(rows) > 0 {
		pg.First = (q.Page-1)*listPageSize + 1
		pg.Last = pg.First + len(rows) - 1
	}
	if q.Page > 1 {
		prev := q
		prev.Page--
		pg.Prev = prev.URL()
	}
	if pg.Last < total {
		next := q
		next.Page++
		pg.Next = next.URL()
	}
	tab := func(text, status string) switchItem {
		t := q
		t.Status, t.Page = status, 1
		return switchItem{Href: t.URL(), Text: text, Current: q.Status == status}
	}
	return map[string]any{
		"Rows": rows, "Error": errMsg, "Total": total, "Query": q, "Pager": pg,
		"Tabs":     []switchItem{tab("All", ""), tab("Active", store.StatusActive), tab("Disabled", store.StatusDisabled)},
		"Headers":  map[string]sortHeader{"phone": q.header("Number", "phone"), "created": q.header("Added", "created"), "last_login": q.header("Last sign-in", "last_login")},
		"Filtered": q.Q != "" || q.Status != "",
	}, nil
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
	var capReader CapacityReader = capacity.Service{Store: d.Store}
	if d.Capacity != nil {
		capReader = d.Capacity
	}
	snap, err := capReader.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return map[string]any{
		"Devices":         linked,
		"ServiceDeviceID": serviceDeviceID,
		"ServiceStatus":   serviceStatus(c, d, serviceDeviceID),
		"Capacity":        snap,
		// Almost full from 90% of the cap; the tile warns (house rule T2:
		// derived here, never stored).
		"NearFull":   snap.Max > 0 && snap.Linked*10 >= snap.Max*9,
		"AllocMB":    mem.Alloc / bytesPerMB,
		"SysMB":      mem.Sys / bytesPerMB,
		"Goroutines": runtime.NumGoroutine(),
		"DiskMB":     diskUsage(d.StorageDir) / bytesPerMB,
		"Error":      errMsg,
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
