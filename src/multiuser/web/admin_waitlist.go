package web

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/capacity"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
)

// CapacityReader is how full the server is (capacity.Service).
type CapacityReader interface {
	Snapshot(ctx context.Context) (capacity.Capacity, error)
	Full(ctx context.Context) bool
}

const listPageSize = store.DefaultPageSize

// waitlistRow is one person on the waitlist as a row shows them, or the row
// after an action (Result says what happened).
type waitlistRow struct {
	Digits      string // the path key: the number without "+"
	Phone       string
	Position    int // 0 on the Declined tab
	Status      string
	Joined      time.Time
	AskedAgain  *time.Time // set when they asked again after joining
	Result      string     // after an action: admitted, sent-failed, exists, declined, reopened, removed, error
	Message     string
	Link        string // the sign-in link to copy when sending failed
	CanAdmit    bool
	ConfirmText string // the line naming them in a confirm dialog
}

func waitlistRows(items []store.WaitlistEntry, offset int, waiting, canAdmit bool) []waitlistRow {
	out := make([]waitlistRow, 0, len(items))
	for i, it := range items {
		r := waitlistRow{Digits: store.DigitsOnly(it.Phone), Phone: it.Phone, Status: it.Status, Joined: it.CreatedAt, CanAdmit: canAdmit}
		if waiting {
			r.Position = offset + i + 1
		}
		if it.LastAskedAt.Sub(it.CreatedAt) > time.Minute {
			t := it.LastAskedAt
			r.AskedAgain = &t
		}
		r.ConfirmText = displayPhone(it.Phone) + ", on the list since " + humanAgo(&r.Joined) + "."
		out = append(out, r)
	}
	return out
}

// waitlistPhone reads the :digits path key: 8 to 15 digits, nothing else.
func waitlistPhone(c fiber.Ctx) (string, error) {
	phone, err := ParseWaitlistNumber(c.Params("digits"))
	if err != nil {
		return "", fiber.ErrNotFound
	}
	return phone, nil
}

// waitlistState is one page of the waitlist and the counts around it.
type waitlistState struct {
	Status                   string
	Page, Offset             int
	Total, Waiting, Declined int
	Items                    []store.WaitlistEntry
	Capacity                 *capacity.Capacity
	Full                     bool
	Counts                   map[string]any // waitlistCounts: the page head and switcher
}

// loadWaitlist reads page pageNo (from 1) of Waiting, or of Declined when
// status says so.
func loadWaitlist(ctx context.Context, d Deps, status string, pageNo int) (waitlistState, error) {
	w := waitlistState{Status: store.WaitlistWaiting, Page: pageNo}
	if status == store.WaitlistDeclined {
		w.Status = store.WaitlistDeclined
	}
	if w.Page < 1 {
		w.Page = 1
	}
	w.Offset = (w.Page - 1) * listPageSize
	var err error
	if w.Total, w.Items, err = d.Store.WaitlistPage(ctx, w.Status, w.Offset, listPageSize); err != nil {
		return w, err
	}
	if w.Counts, err = waitlistCounts(ctx, d, w.Status); err != nil {
		return w, err
	}
	w.Waiting, _ = w.Counts["Waiting"].(int)
	w.Declined, _ = w.Counts["Declined"].(int)
	if snap, ok := w.Counts["Capacity"].(capacity.Capacity); ok {
		w.Capacity = &snap
		w.Full = snap.Max > 0 && snap.Linked >= snap.Max
	}
	return w, nil
}

// mountWaitlist registers Admin > Waitlist: the list (GET /admin/waitlist,
// ?status=declined, ?page=N) and one action per POST, each answering htmx
// with the row it changed. Superadmin only (404 to anyone else), CSRF on
// every POST. Let in is one person per click; nothing here lets in many.
func mountWaitlist(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	admin := d.Auth.RequireSuperadmin()
	csrf := d.Auth.VerifyCSRF()

	app.Get("/admin/waitlist", page, admin, func(c fiber.Ctx) error {
		pageNo, _ := strconv.Atoi(c.Query("page"))
		w, err := loadWaitlist(c.Context(), d, c.Query("status"), pageNo)
		if err != nil {
			return err
		}
		status := w.Status
		data := w.Counts
		data["Total"] = w.Total
		data["Full"] = w.Full
		data["Rows"] = waitlistRows(w.Items, w.Offset, status == store.WaitlistWaiting, !w.Full)
		base := "/admin/waitlist?"
		if status == store.WaitlistDeclined {
			base += "status=declined&"
		}
		data["Pager"] = newPager(base, w.Page, len(w.Items), w.Total)
		return d.render(c, fiber.StatusOK, "admin_waitlist.html", "Waitlist", data)
	})

	// row answers an action with the row again and, out of band, the counts
	// it changed. The tab the page shows comes from htmx's HX-Current-URL.
	row := func(c fiber.Ctx, r waitlistRow) error {
		status := store.WaitlistWaiting
		if u, err := url.Parse(c.Get("HX-Current-URL")); err == nil && u.Query().Get("status") == store.WaitlistDeclined {
			status = store.WaitlistDeclined
		}
		counts, err := waitlistCounts(c.Context(), d, status)
		if err != nil {
			return err
		}
		counts["OOB"] = true
		return d.render(c, fiber.StatusOK, "waitlist_row.html", "", map[string]any{"Row": r, "Counts": counts})
	}

	app.Post("/admin/waitlist/:digits/admit", page, admin, csrf, func(c fiber.Ctx) error {
		phone, err := waitlistPhone(c)
		if err != nil {
			return err
		}
		caller, _ := userOf(c)
		out, err := d.AdminAdmit(c.Context(), caller, phone, c.IP())
		if err != nil {
			return err
		}
		r := waitlistRow{Digits: store.DigitsOnly(phone), Phone: phone, Status: store.WaitlistAdmitted,
			Result: out.Result, Message: out.Message, Link: out.Link}
		return row(c, r)
	})

	decide := func(action string) fiber.Handler {
		return func(c fiber.Ctx) error {
			phone, err := waitlistPhone(c)
			if err != nil {
				return err
			}
			caller, _ := userOf(c)
			out, err := d.AdminDecideWaitlist(c.Context(), caller, phone, action, c.IP())
			if err != nil {
				return err
			}
			r := waitlistRow{Digits: store.DigitsOnly(phone), Phone: phone, Result: out.Result, Message: out.Message}
			switch action {
			case "decline":
				r.Status = store.WaitlistDeclined
			case "reopen":
				r.Status = store.WaitlistWaiting
			}
			return row(c, r)
		}
	}
	app.Post("/admin/waitlist/:digits/decline", page, admin, csrf, decide("decline"))
	app.Post("/admin/waitlist/:digits/reopen", page, admin, csrf, decide("reopen"))
	app.Post("/admin/waitlist/:digits/remove", page, admin, csrf, decide("remove"))
}

// switchItem is one choice of a navigating switcher (house rule B4).
// waitlistCounts is what the page head, the Waiting / Declined switcher and
// the menu count show, for the tab status: Status, Total (that tab's
// count), Waiting, Declined, Tabs and, with a cap, Capacity.
func waitlistCounts(ctx context.Context, d Deps, status string) (map[string]any, error) {
	waitingN, err := d.Store.WaitlistCount(ctx, store.WaitlistWaiting)
	if err != nil {
		return nil, err
	}
	declinedN, err := d.Store.WaitlistCount(ctx, store.WaitlistDeclined)
	if err != nil {
		return nil, err
	}
	total := waitingN
	if status == store.WaitlistDeclined {
		total = declinedN
	}
	data := map[string]any{"Status": status, "Total": total, "Waiting": waitingN, "Declined": declinedN,
		"Tabs": []switchItem{
			{Href: "/admin/waitlist", Text: "Waiting", Count: waitingN, Current: status == store.WaitlistWaiting},
			{Href: "/admin/waitlist?status=declined", Text: "Declined", Count: declinedN, Current: status == store.WaitlistDeclined},
		}}
	if d.Capacity != nil {
		snap, err := d.Capacity.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		data["Capacity"] = snap
	}
	return data, nil
}

type switchItem struct {
	Href    string
	Text    string
	Count   int
	Current bool
}

// pager is the Previous / Next pair under a list and what it shows.
type pager struct {
	First, Last, Total int
	Prev, Next         string
}

func newPager(base string, pageNo, drawn, total int) pager {
	p := pager{Total: total}
	if drawn > 0 {
		p.First = (pageNo-1)*listPageSize + 1
		p.Last = p.First + drawn - 1
	}
	if pageNo > 1 {
		p.Prev = base + "page=" + strconv.Itoa(pageNo-1)
		if pageNo == 2 {
			p.Prev = trimQuery(base)
		}
	}
	if p.Last < total {
		p.Next = base + "page=" + strconv.Itoa(pageNo+1)
	}
	return p
}

// trimQuery drops a dangling "?" or "&" from a base URL.
func trimQuery(base string) string {
	for len(base) > 0 && (base[len(base)-1] == '?' || base[len(base)-1] == '&') {
		base = base[:len(base)-1]
	}
	return base
}
