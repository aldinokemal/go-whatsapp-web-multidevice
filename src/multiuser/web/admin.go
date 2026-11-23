package web

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"path/filepath"
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

// OneTimeLinkNote is how every one-time sign-in link is described: Add
// user, Get sign-in link and the admin-link command.
const OneTimeLinkNote = "It works once, for 15 minutes."

// userRowURL is where a person's row on Users is found from anywhere: the
// list searched by their number (their row may be on any page), scrolled to
// the row, which is highlighted.
func userRowURL(u *store.User) string {
	return "/admin/users?q=" + store.DigitsOnly(u.Phone) + "#user-" + u.ID
}

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
		caller, _ := userOf(c)
		typed := strings.Clone(c.FormValue("phone"))
		phone, link, id, err := d.AdminAddUser(c.Context(), caller, typed, c.IP())
		var r *Refusal
		if errors.As(err, &r) {
			return d.render(c, fiber.StatusOK, "add_user.html", "", map[string]any{"Phone": typed, "Error": r.Msg})
		}
		if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "add_user.html", "", map[string]any{"Link": link, "Phone": phone, "ID": id})
	})

	app.Post("/admin/users/:id/status", page, admin, csrf, func(c fiber.Ctx) error {
		caller, _ := userOf(c)
		// The confirm dialog's "Also unlink their numbers" box (on by
		// default). An unticked box sends nothing, which keeps them.
		err := d.AdminSetStatus(c.Context(), caller, c.Params("id"), c.FormValue("status"), c.FormValue("unlink") == "on", c.IP())
		var r *Refusal
		if errors.As(err, &r) {
			return renderUsers(c, d, r.Msg)
		}
		if errors.Is(err, store.ErrNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/admin/users")
	})

	app.Post("/admin/users/:id/signin-link", page, admin, csrf, func(c fiber.Ctx) error {
		caller, _ := userOf(c)
		link, err := d.AdminSigninLink(c.Context(), caller, c.Params("id"))
		var r *Refusal
		if errors.As(err, &r) {
			return d.render(c, fiber.StatusOK, "signin_link.html", "", map[string]any{"Error": r.Msg})
		}
		if errors.Is(err, store.ErrNotFound) {
			return fiber.ErrNotFound
		}
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
		err := d.AdminSetSigninNumber(c.Context(), caller, c.FormValue("device_id"), c.IP())
		var r *Refusal
		if errors.As(err, &r) {
			return renderSettings(c, d, r.Msg)
		}
		if err != nil {
			return err
		}
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
func signinLinkFor(ctx context.Context, d Deps, phone string) (string, error) {
	raw, err := d.Store.CreateLoginToken(ctx, phone, signinLinkTTL)
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
	Highlight   bool // the row the link was for: the one just added, or the exact number searched
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
	New    string // a user just added: pinned first, highlighted (not kept in links)
}

func parseUsersQuery(c fiber.Ctx) usersQuery {
	q := usersQuery{Q: strings.TrimSpace(c.Query("q")), Status: c.Query("status"), Sort: c.Query("sort"), Desc: c.Query("dir") == "desc", New: c.Query("new")}
	q.Page, _ = strconv.Atoi(c.Query("page"))
	return q.clean()
}

// clean keeps only the statuses and sorts the list knows, and a page from 1.
func (q usersQuery) clean() usersQuery {
	if q.Status != store.StatusActive && q.Status != store.StatusDisabled {
		q.Status = ""
	}
	switch q.Sort {
	case "created", "last_login", "phone":
	default:
		q.Sort, q.Desc = "", false
	}
	if q.Page < 1 {
		q.Page = 1
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

// usersPage is one page of Users for caller, as rows.
func usersPage(ctx context.Context, d Deps, q usersQuery, caller *store.User) (int, []store.User, []userRow, error) {
	total, users, err := d.Store.ListUsersPage(ctx, store.UsersQuery{Search: q.Q, Status: q.Status, Sort: q.Sort,
		Desc: q.Desc, Offset: (q.Page - 1) * listPageSize, Limit: listPageSize})
	if err != nil {
		return 0, nil, nil, err
	}
	// House rule D4: after Add user the new row is shown on top, whatever
	// the list's filter, sort or page would have done with it.
	if q.New != "" {
		if nu, err := d.Store.UserByID(ctx, q.New); err == nil {
			kept := []store.User{*nu}
			for _, u := range users {
				if u.ID != nu.ID {
					kept = append(kept, u)
				}
			}
			users = kept
		} else if !errors.Is(err, store.ErrNotFound) {
			return 0, nil, nil, err
		} else {
			q.New = ""
		}
	}
	rows, err := userRows(ctx, d, users, caller)
	if err != nil {
		return 0, nil, nil, err
	}
	return total, users, rows, nil
}

func usersPageData(c fiber.Ctx, d Deps, errMsg string) (map[string]any, error) {
	q := parseUsersQuery(c)
	caller, _ := userOf(c)
	total, _, rows, err := usersPage(c.Context(), d, q, caller)
	if err != nil {
		return nil, err
	}
	searched := store.DigitsOnly(q.Q)
	for i := range rows {
		rows[i].Highlight = (q.New != "" && rows[i].ID == q.New) ||
			(len(searched) >= 8 && store.DigitsOnly(rows[i].Phone) == searched)
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

// settingsView is what Settings shows, derived at request time.
type settingsView struct {
	Devices         []domainDevice.Device
	ServiceDeviceID string
	ServiceStatus   string
	Capacity        capacity.Capacity
	NearFull        bool
	AllocMB, SysMB  uint64
	Goroutines      int
	DiskMB          int64
}

func (d Deps) capacityReader() CapacityReader {
	if d.Capacity != nil {
		return d.Capacity
	}
	return capacity.Service{Store: d.Store}
}

// nearFull: almost full from 90% of the cap; the tile warns (house rule T2:
// derived here, never stored).
func nearFull(snap capacity.Capacity) bool { return snap.Max > 0 && snap.Linked*10 >= snap.Max*9 }

func settingsData(ctx context.Context, d Deps) (settingsView, error) {
	devices, err := d.Devices.ListDevices(ctx)
	if err != nil {
		return settingsView{}, err
	}
	linked := make([]domainDevice.Device, 0, len(devices))
	for _, dev := range devices {
		if dev.PhoneNumber != "" {
			linked = append(linked, dev)
		}
	}
	serviceDeviceID, _, err := d.Store.Setting(ctx, store.SettingServiceDeviceID)
	if err != nil {
		return settingsView{}, err
	}
	snap, err := d.capacityReader().Snapshot(ctx)
	if err != nil {
		return settingsView{}, err
	}
	alloc, sys, goroutines := memStats()
	return settingsView{Devices: linked, ServiceDeviceID: serviceDeviceID, ServiceStatus: serviceStatus(ctx, d, serviceDeviceID),
		Capacity: snap, NearFull: nearFull(snap), AllocMB: alloc, SysMB: sys, Goroutines: goroutines,
		DiskMB: diskUsage(d.StorageDir) / bytesPerMB}, nil
}

func settingsPageData(c fiber.Ctx, d Deps, errMsg string) (map[string]any, error) {
	s, err := settingsData(c.Context(), d)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Devices":         s.Devices,
		"ServiceDeviceID": s.ServiceDeviceID,
		"ServiceStatus":   s.ServiceStatus,
		"Capacity":        s.Capacity,
		"NearFull":        s.NearFull,
		"AllocMB":         s.AllocMB,
		"SysMB":           s.SysMB,
		"Goroutines":      s.Goroutines,
		"DiskMB":          s.DiskMB,
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
func serviceStatus(ctx context.Context, d Deps, id string) string {
	switch {
	case id == "":
		return "Not set"
	case d.ServicePhone != nil && d.ServicePhone(ctx) != "":
		return "Connected"
	default:
		return "Not connected"
	}
}
