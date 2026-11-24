package web

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
)

// sessionView is one of the signed-in user's own sessions as Account lists
// it. Handle is an opaque HMAC of the stored hash (auth.SessionHandle): the
// raw session id and its hash never reach HTML.
type sessionView struct {
	Handle   string
	Device   string
	Started  time.Time
	LastSeen *time.Time
	Current  bool
	Phone    bool // a phone or tablet, for the row's icon
}

func sessionViews(c fiber.Ctx, d Deps, userID string) ([]sessionView, error) {
	return sessionList(c.Context(), d, userID, d.Auth.CurrentSessionHash(c))
}

// sessionList is userID's sessions; current (a stored hash, or "") marks
// the one in use and puts it first.
func sessionList(ctx context.Context, d Deps, userID, current string) ([]sessionView, error) {
	list, err := d.Store.SessionsOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]sessionView, 0, len(list))
	for _, s := range list {
		v := sessionView{Handle: d.Auth.SessionHandle(s.IDHash), Device: deviceName(s.UserAgent),
			Started: s.CreatedAt, LastSeen: s.LastSeenAt, Current: current != "" && s.IDHash == current,
			Phone: strings.Contains(s.UserAgent, "Mobile") || strings.Contains(s.UserAgent, "Android") || strings.Contains(s.UserAgent, "iPhone")}
		if v.Device == "This device" { // deviceName's fallback; here it would mislead
			v.Device = "Unknown browser"
		}
		out = append(out, v)
	}
	// The session you're using goes first.
	for i, v := range out {
		if v.Current && i > 0 {
			copy(out[1:i+1], out[0:i])
			out[0] = v
			break
		}
	}
	return out, nil
}

// mountSessions registers per-session sign-out on Account. The handle is
// matched only against the signed-in user's own sessions, so another user's
// session can never be named, let alone ended: it is simply not found.
func mountSessions(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	csrf := d.Auth.VerifyCSRF()

	app.Post("/account/sessions/:handle/signout", page, csrf, func(c fiber.Ctx) error {
		u, _ := userOf(c)
		list, err := d.Store.SessionsOf(c.Context(), u.ID)
		if err != nil {
			return err
		}
		handle := c.Params("handle")
		for _, s := range list {
			if d.Auth.SessionHandle(s.IDHash) != handle {
				continue
			}
			if s.IDHash == d.Auth.CurrentSessionHash(c) {
				if err := d.Auth.EndSession(c); err != nil {
					return err
				}
				_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "session_ended", IP: c.IP()})
				return c.Redirect().Status(fiber.StatusSeeOther).To("/signin")
			}
			if err := d.Store.DeleteUserSession(c.Context(), u.ID, s.IDHash); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fiber.ErrNotFound
				}
				return err
			}
			_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "session_ended", IP: c.IP()})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/account")
		}
		return fiber.ErrNotFound
	})
}
