package web

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/capacity"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
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
	digits := c.Params("digits")
	if digits == "" || store.DigitsOnly(digits) != digits {
		return "", fiber.ErrNotFound
	}
	phone, err := store.NormalizePhone("+" + digits)
	if err != nil || store.DigitsOnly(phone) != digits {
		return "", fiber.ErrNotFound
	}
	return phone, nil
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
		ctx := c.Context()
		status := store.WaitlistWaiting
		if c.Query("status") == store.WaitlistDeclined {
			status = store.WaitlistDeclined
		}
		pageNo, _ := strconv.Atoi(c.Query("page"))
		if pageNo < 1 {
			pageNo = 1
		}
		offset := (pageNo - 1) * listPageSize
		total, items, err := d.Store.WaitlistPage(ctx, status, offset, listPageSize)
		if err != nil {
			return err
		}
		waitingN, err := d.Store.WaitlistCount(ctx, store.WaitlistWaiting)
		if err != nil {
			return err
		}
		declinedN, err := d.Store.WaitlistCount(ctx, store.WaitlistDeclined)
		if err != nil {
			return err
		}
		data := map[string]any{"Status": status, "Total": total, "Waiting": waitingN, "Declined": declinedN}
		full := false
		if d.Capacity != nil {
			snap, err := d.Capacity.Snapshot(ctx)
			if err != nil {
				return err
			}
			data["Capacity"] = snap
			full = snap.Max > 0 && snap.Linked >= snap.Max
		}
		data["Full"] = full
		data["Rows"] = waitlistRows(items, offset, status == store.WaitlistWaiting, !full)
		base := "/admin/waitlist?"
		if status == store.WaitlistDeclined {
			base += "status=declined&"
		}
		data["Pager"] = newPager(base, pageNo, len(items), total)
		data["Tabs"] = []switchItem{
			{Href: "/admin/waitlist", Text: "Waiting", Count: waitingN, Current: status == store.WaitlistWaiting},
			{Href: "/admin/waitlist?status=declined", Text: "Declined", Count: declinedN, Current: status == store.WaitlistDeclined},
		}
		return d.render(c, fiber.StatusOK, "admin_waitlist.html", "Waitlist", data)
	})

	row := func(c fiber.Ctx, r waitlistRow) error {
		return d.render(c, fiber.StatusOK, "waitlist_row.html", "", r)
	}

	app.Post("/admin/waitlist/:digits/admit", page, admin, csrf, func(c fiber.Ctx) error {
		phone, err := waitlistPhone(c)
		if err != nil {
			return err
		}
		ctx := c.Context()
		caller, _ := userOf(c)
		r := waitlistRow{Digits: store.DigitsOnly(phone), Phone: phone, Status: store.WaitlistAdmitted}
		if d.Capacity != nil && d.Capacity.Full(ctx) {
			r.Result, r.Message = "error", "The server is full. Free a number or raise the limit first."
			return row(c, r)
		}
		limit := d.DefaultMaxDevices
		if limit < 1 {
			limit = 2
		}
		u, created, err := d.Store.AdmitFromWaitlist(ctx, phone, caller.ID, limit)
		switch {
		case errors.Is(err, store.ErrNotFound):
			r.Result, r.Message = "error", "This number isn't on the waitlist any more."
			return row(c, r)
		case errors.Is(err, store.ErrInvalid):
			r.Result, r.Message = "error", "This number was declined. Move it back to waiting first."
			return row(c, r)
		case err != nil:
			return err
		}
		if !created {
			r.Result, r.Message = "exists", "This number already has an account. Nothing was sent."
			return row(c, r)
		}
		_ = d.Store.Audit(ctx, store.AuditEvent{UserID: u.ID, ActorID: caller.ID, Action: "waitlist_admitted", IP: c.IP()})
		link, err := signinLinkFor(c, d, phone)
		if err != nil {
			return err
		}
		text := "You're in. Here's your " + d.ProductName + " sign-in link. It works once, for 15 minutes:\n\n" + link
		if d.SendWhatsApp == nil {
			r.Result, r.Link = "sent-failed", link
		} else if err := d.SendWhatsApp(ctx, phone, text); err != nil {
			logrus.WithError(err).Warn("[WTF] waitlist: could not send the sign-in link")
			r.Result, r.Link = "sent-failed", link
		} else {
			r.Result = "admitted"
		}
		return row(c, r)
	})

	decide := func(action string) fiber.Handler {
		return func(c fiber.Ctx) error {
			phone, err := waitlistPhone(c)
			if err != nil {
				return err
			}
			ctx := c.Context()
			caller, _ := userOf(c)
			r := waitlistRow{Digits: store.DigitsOnly(phone), Phone: phone}
			switch action {
			case "decline":
				err = d.Store.DeclineWaitlist(ctx, phone, caller.ID)
				r.Result, r.Status = "declined", store.WaitlistDeclined
			case "reopen":
				err = d.Store.ReopenWaitlist(ctx, phone)
				r.Result, r.Status = "reopened", store.WaitlistWaiting
			case "remove":
				err = d.Store.RemoveFromWaitlist(ctx, phone)
				r.Result = "removed"
			}
			if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
				r.Result, r.Message = "error", "This number has moved on. Reload the page."
				return row(c, r)
			}
			if err != nil {
				return err
			}
			_ = d.Store.Audit(ctx, store.AuditEvent{ActorID: caller.ID, Action: map[string]string{
				"decline": "waitlist_declined", "reopen": "waitlist_reopened", "remove": "waitlist_removed"}[action], IP: c.IP()})
			return row(c, r)
		}
	}
	app.Post("/admin/waitlist/:digits/decline", page, admin, csrf, decide("decline"))
	app.Post("/admin/waitlist/:digits/reopen", page, admin, csrf, decide("reopen"))
	app.Post("/admin/waitlist/:digits/remove", page, admin, csrf, decide("remove"))
}

// switchItem is one choice of a navigating switcher (house rule B4).
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
