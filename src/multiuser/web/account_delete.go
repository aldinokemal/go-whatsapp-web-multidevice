package web

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/account"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
)

// The typed-confirmation label and its error say the same thing.
const (
	typeLabel          = "Type your number or DELETE"
	typeToConfirm      = typeLabel + " to confirm."
	typeTheirLabel     = "Type their number to confirm"
	typeTheirToConfirm = typeTheirLabel + "."
)

// deleteView is what the Delete account card and its dialog show, for the
// person's own account or (Their) for an admin deleting someone else's.
type deleteView struct {
	Their       bool   // an admin deleting another account
	UserID      string // whose account (Their only)
	Phone       string // their number (Their only)
	OwnsService bool   // the account holds the sign-in number: refused
	Numbers     int    // how many numbers are unlinked
	Error       string
	ErrorLink   bool // the error points to Settings
	Invalid     bool // what was typed does not confirm
}

// deleteAccountData describes the card for u, or nil when deleting is off.
func deleteAccountData(ctx context.Context, d Deps, u *store.User) (*deleteView, error) {
	if d.DeleteAccount == nil {
		return nil, nil
	}
	ids, err := d.Store.DevicesOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	svc, _, err := d.Store.Setting(ctx, store.SettingServiceDeviceID)
	if err != nil {
		return nil, err
	}
	v := &deleteView{Numbers: len(ids)}
	for _, id := range ids {
		v.OwnsService = v.OwnsService || (svc != "" && id == svc)
	}
	return v, nil
}

// confirmsDeletion: the person typed their own number (any spelling) or
// DELETE (any case).
func confirmsDeletion(typed, phone string) bool {
	typed = strings.TrimSpace(typed)
	if strings.EqualFold(typed, "DELETE") {
		return true
	}
	d := store.DigitsOnly(typed)
	return len(d) >= 8 && d == store.DigitsOnly(phone)
}

// mountAccountDelete registers POST /account/delete (a signed-in session and
// CSRF; an API key is not enough). The dialog posts with htmx: an error
// comes back as the dialog's form again, success as HX-Redirect to sign-in.
func mountAccountDelete(app *fiber.App, d Deps) {
	app.Post("/account/delete", d.Auth.RequirePage(), d.Auth.VerifyCSRF(), func(c fiber.Ctx) error {
		if d.DeleteAccount == nil {
			return fiber.ErrNotFound
		}
		u, _ := userOf(c)
		ctx := c.Context()
		v, err := deleteAccountData(ctx, d, u)
		if err != nil {
			return err
		}
		again := func(msg string, link bool) error {
			v.Error, v.ErrorLink = msg, link
			v.Invalid = msg == typeToConfirm
			if c.Get("HX-Request") != "true" {
				return c.Redirect().Status(fiber.StatusSeeOther).To("/account")
			}
			return d.render(c, fiber.StatusOK, "delete_account_form.html", "", v)
		}
		if !confirmsDeletion(c.FormValue("confirm"), u.Phone) {
			return again(typeToConfirm, false)
		}
		if err := d.DeleteAccount(ctx, u.ID); err != nil {
			if errors.Is(err, account.ErrOwnsServiceNumber) {
				return again("", true)
			}
			// Stopped half way: the account is already refused everywhere and
			// start-up finishes it. Anything earlier changed nothing.
			if cur, gerr := d.Store.UserByID(ctx, u.ID); gerr == nil && cur.Status != store.StatusDeleting {
				logrus.WithError(err).Warnf("[WTF] delete account %s", u.ID)
				return again("We couldn't delete your account. Try again.", false)
			}
			logrus.WithError(err).Warnf("[WTF] delete account %s: left to finish at start-up", u.ID)
		}
		if err := d.Auth.EndSession(c); err != nil && !errors.Is(err, store.ErrNotFound) {
			logrus.WithError(err).Warn("[WTF] delete account: end session")
		}
		const to = "/signin?deleted=1"
		if c.Get("HX-Request") == "true" {
			c.Set("HX-Redirect", to)
			return c.SendStatus(fiber.StatusOK)
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To(to)
	})
}

// Items says plainly what deleting removes.
func (v deleteView) Items() []string {
	whose, they := "Your", "you"
	if v.Their {
		whose, they = "Their", "they"
	}
	var nums string
	switch v.Numbers {
	case 0:
	case 1:
		nums = whose + " 1 linked WhatsApp number: unlinked, with its messages and media here"
	default:
		nums = whose + " " + strconv.Itoa(v.Numbers) + " linked WhatsApp numbers: unlinked, with their messages and media here"
	}
	out := []string{whose + " account and how " + they + " sign in: passkeys, sessions, API keys and connected apps"}
	if nums != "" {
		out = append(out, nums)
	}
	return out
}

// deleteConfirm is the confirm dialog for Delete account, built once for the
// page and for the htmx answer that redraws its form.
func deleteConfirm(v *deleteView, csrf string) map[string]any {
	m := map[string]any{
		"ID": "delete-account", "Action": "/account/delete", "CSRF": csrf, "Icon": "trash-2",
		"Title": "Delete your account?", "Items": v.Items(),
		"Body":  "This can't be undone. Your WhatsApp account and the chats on your phone stay as they are.",
		"Typed": typeLabel, "TypedName": "confirm",
		"Button": "Delete account", "Busy": "Deleting…", "Error": v.Error, "Invalid": v.Invalid,
	}
	if v.ErrorLink {
		m["Error"] = "Your account has the sign-in number."
		m["ErrorHref"], m["ErrorLinkText"] = "/admin/settings", "Choose another one in Settings"
	}
	return m
}

// adminDeleteConfirm is the confirm dialog for an admin's Delete account on
// a disabled user's row, built once for the page and for the htmx answer
// that redraws its form: the same list of what goes, their number to type.
func adminDeleteConfirm(v *deleteView, csrf string) map[string]any {
	m := map[string]any{
		"ID": "delete-" + v.UserID, "Action": "/admin/users/" + v.UserID + "/delete", "CSRF": csrf, "Icon": "trash-2",
		"Title": "Delete " + displayPhone(v.Phone) + "?", "Items": v.Items(),
		"Body":  "This can't be undone. Their WhatsApp account and the chats on their phone stay as they are.",
		"Typed": typeTheirLabel, "TypedName": "confirm",
		"Button": "Delete account", "Busy": "Deleting…", "Error": v.Error, "Invalid": v.Invalid,
	}
	if v.ErrorLink {
		m["Error"] = "This account has the sign-in number."
		m["ErrorHref"], m["ErrorLinkText"] = "/admin/settings", "Choose another one in Settings"
	}
	return m
}

// confirmsTheirNumber: the admin typed the account's own number (any
// spelling). DELETE alone is not enough when it is someone else's account.
func confirmsTheirNumber(typed, phone string) bool {
	d := store.DigitsOnly(strings.TrimSpace(typed))
	return len(d) >= 8 && d == store.DigitsOnly(phone)
}
