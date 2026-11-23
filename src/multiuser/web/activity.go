package web

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
)

const activityPageSize = 50

// wordPart is one piece of an activity sentence: plain words, or a person
// (Href set) rendered as a link to their row on Users.
type wordPart struct {
	Text string
	Href string
}

type activityRow struct {
	At    time.Time
	Parts []wordPart
}

// mountActivity registers the superadmin's Activity page: the audit log in
// plain words, newest first, 50 a page, optionally for one user.
func mountActivity(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	admin := d.Auth.RequireSuperadmin()

	app.Get("/admin/activity", page, admin, func(c fiber.Ctx) error {
		ctx := c.Context()
		pageNo, _ := strconv.Atoi(c.Query("page"))
		if pageNo < 1 {
			pageNo = 1
		}
		userID := c.Query("user")
		var who *store.User
		if userID != "" {
			u, err := d.Store.UserByID(ctx, userID)
			if err != nil {
				return fiber.ErrNotFound
			}
			who = u
		}
		events, total, err := d.Store.ListAudit(ctx, store.AuditQuery{UserID: userID, Limit: activityPageSize,
			Offset: (pageNo - 1) * activityPageSize})
		if err != nil {
			return err
		}
		rows, err := activityRows(ctx, d.Store, events)
		if err != nil {
			return err
		}
		first, last := 0, 0
		if len(rows) > 0 {
			first = (pageNo-1)*activityPageSize + 1
			last = first + len(rows) - 1
		}
		base := "/admin/activity?"
		if userID != "" {
			base += "user=" + userID + "&"
		}
		data := map[string]any{"Rows": rows, "Total": total, "First": first, "Last": last, "Who": who}
		if who != nil {
			data["WhoHref"] = userRowURL(who)
		}
		if pageNo > 1 {
			data["Prev"] = base + "page=" + strconv.Itoa(pageNo-1)
		}
		if last < total {
			data["Next"] = base + "page=" + strconv.Itoa(pageNo+1)
		}
		return d.render(c, fiber.StatusOK, "admin_activity.html", "Activity", data)
	})
}

func activityRows(ctx context.Context, st *store.Store, events []store.AuditRow) ([]activityRow, error) {
	var ids []string
	for _, e := range events {
		ids = append(ids, e.UserID, e.ActorID)
	}
	users, err := st.UsersByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]activityRow, 0, len(events))
	for _, e := range events {
		out = append(out, activityRow{At: e.At, Parts: describe(e, users)})
	}
	return out, nil
}

// describe turns one audit event into a sentence. Only ids, numbers and the
// non-secret Detail feed it; nothing else is stored in the log.
func describe(e store.AuditRow, users map[string]store.User) []wordPart {
	person := func(id string) wordPart {
		if u, ok := users[id]; ok {
			return wordPart{Text: displayPhone(u.Phone), Href: userRowURL(&u)}
		}
		if id == "" {
			return wordPart{Text: "Someone"}
		}
		return wordPart{Text: "a removed user"}
	}
	w := func(s string) wordPart { return wordPart{Text: s} }
	u := person(e.UserID)
	hasActor := e.ActorID != "" && e.ActorID != e.UserID
	a := person(e.ActorID)
	number := w("a number")
	if e.Detail != "" && (e.Action == "device_remove") {
		number = w(displayPhone(e.Detail))
	}

	switch e.Action {
	case "signin":
		return []wordPart{u, w(" signed in")}
	case "signin_passkey":
		return []wordPart{u, w(" signed in with a passkey")}
	case "signup":
		return []wordPart{u, w(" joined")}
	case "signin_link_sent":
		return []wordPart{u, w(" was sent a sign-in link on WhatsApp")}
	case "admin_link_minted":
		return []wordPart{w("A sign-in link was made for "), u, w(" on the server")}
	case "passkey_add":
		return []wordPart{u, w(" added a passkey")}
	case "passkey_clone_warning":
		return []wordPart{w("A passkey of "), u, w(" may have been copied")}
	case "session_ended":
		return []wordPart{u, w(" signed out one session")}
	case "api_key_created":
		return []wordPart{u, w(" made an API key")}
	case "api_key_revoked":
		return []wordPart{u, w(" revoked an API key")}
	case "sessions_ended_all":
		return []wordPart{u, w(" signed out everywhere")}
	case "device_add":
		return []wordPart{u, w(" started linking a number")}
	case "device_remove":
		if hasActor {
			return []wordPart{w("Admin "), a, w(" unlinked "), number, w(" from "), u}
		}
		return []wordPart{u, w(" unlinked "), number}
	case "device_slot_expired":
		return []wordPart{w("An unfinished link of "), u, w(" was removed after an hour")}
	case "device_renamed":
		return []wordPart{u, w(" renamed a number")}
	case "device_link_refused_duplicate":
		return []wordPart{u, w(" tried to link a number that's already linked to another account")}
	case "device_link_refused_unreadable_number", "device_link_refused_lookup_failed":
		return []wordPart{w("A link by "), u, w(" was refused: the number couldn't be checked")}
	case "user_disabled":
		if hasActor {
			return []wordPart{w("Admin "), a, w(" disabled "), u}
		}
		return []wordPart{u, w(" was disabled")}
	case "user_enabled":
		if hasActor {
			return []wordPart{w("Admin "), a, w(" enabled "), u}
		}
		return []wordPart{u, w(" was enabled")}
	case "user_limit_set":
		return []wordPart{w("Admin "), a, w(" let "), u, w(" link up to " + e.Detail + " numbers")}
	case "user_phone_changed":
		old, _, _ := strings.Cut(e.Detail, " ")
		return []wordPart{w("Admin "), a, w(" changed the number of "), u, w(" (was " + displayPhone(old) + ")")}
	case "user_added":
		if e.ActorID != "" {
			return []wordPart{w("Admin "), a, w(" added "), u}
		}
		return []wordPart{u, w(" was added")}
	case "user_deleted":
		return []wordPart{w("Admin "), a, w(" deleted an account")}
	case "account_deleted":
		return []wordPart{w("An account was deleted")}
	case "waitlist_joined":
		return []wordPart{w("Someone joined the waitlist")}
	case "waitlist_admitted":
		return []wordPart{w("Admin "), a, w(" let "), u, w(" in from the waitlist")}
	case "waitlist_declined":
		return []wordPart{w("Admin "), a, w(" declined a number on the waitlist")}
	case "waitlist_reopened":
		return []wordPart{w("Admin "), a, w(" moved a number back to waiting")}
	case "waitlist_removed":
		return []wordPart{w("Admin "), a, w(" removed a number from the waitlist")}
	case "service_number_set":
		return []wordPart{u, w(" chose the number that sends sign-in links")}
	case "webhook_set":
		return []wordPart{u, w(" set a webhook for a number")}
	case "webhook_cleared":
		return []wordPart{u, w(" removed the webhook from a number")}
	}
	return []wordPart{u, w(": " + strings.ReplaceAll(e.Action, "_", " "))}
}
