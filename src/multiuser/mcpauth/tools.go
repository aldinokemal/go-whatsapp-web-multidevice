package mcpauth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/web"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	mcpg "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
)

// The WTF tools (house rule S5: whatever the GUI can do, MCP can do). Each
// one calls the very web.Deps action or owner-scoped usecase the page calls.
// Deliberately absent (docs/mcp-parity.md): making or revoking API keys,
// adding or removing passkeys, ending sessions, disconnecting apps and
// deleting your own account. Those stay in the browser with a person there.
const (
	MyDevicesTool = "wtf_my_devices"

	LinkNumberTool      = "wtf_link_number"
	NumberStatusTool    = "wtf_number_status"
	ReconnectNumberTool = "wtf_reconnect_number"
	UnlinkNumberTool    = "wtf_unlink_number"
	RenameNumberTool    = "wtf_rename_number"
	GetWebhookTool      = "wtf_get_webhook"
	SetWebhookTool      = "wtf_set_webhook"
	RemoveWebhookTool   = "wtf_remove_webhook"

	MyAccountTool       = "wtf_my_account"
	MySessionsTool      = "wtf_my_sessions"
	MyConnectedAppsTool = "wtf_my_connected_apps"
	MyAPIKeysTool       = "wtf_my_api_keys"
	MyPasskeysTool      = "wtf_my_passkeys"

	AdminListUsersTool        = "wtf_admin_list_users"
	AdminAddUserTool          = "wtf_admin_add_user"
	AdminSigninLinkTool       = "wtf_admin_signin_link"
	AdminSetUserStatusTool    = "wtf_admin_set_user_status"
	AdminSetNumberLimitTool   = "wtf_admin_set_number_limit"
	AdminChangeNumberTool     = "wtf_admin_change_number"
	AdminUnlinkNumberTool     = "wtf_admin_unlink_number"
	AdminDeleteUserTool       = "wtf_admin_delete_user"
	AdminActivityTool         = "wtf_admin_activity"
	AdminWaitlistTool         = "wtf_admin_waitlist"
	AdminWaitlistDecideTool   = "wtf_admin_waitlist_decide"
	AdminSettingsTool         = "wtf_admin_settings"
	AdminSetSigninNumberTool  = "wtf_admin_set_signin_number"
	AdminCapacityTool         = "wtf_admin_capacity"
	adminPrefix               = "wtf_admin_"
	AdminOnlyMessage          = "This tool is only for the server's admins."
	deviceNotFoundMessage     = "device not found"
	notFoundMessage           = "not found"
	somethingWentWrongMessage = "Something went wrong at our end. Try again."
)

// AdminTools is every admin tool, for the tools/list filter and the tests.
var AdminTools = []string{AdminListUsersTool, AdminAddUserTool, AdminSigninLinkTool, AdminSetUserStatusTool,
	AdminSetNumberLimitTool, AdminChangeNumberTool, AdminUnlinkNumberTool, AdminDeleteUserTool, AdminActivityTool,
	AdminWaitlistTool, AdminWaitlistDecideTool, AdminSettingsTool, AdminSetSigninNumberTool, AdminCapacityTool}

type myDevice struct {
	DeviceID string `json:"device_id"`
	Phone    string `json:"phone,omitempty"`
	Name     string `json:"name,omitempty"`
	Label    string `json:"label,omitempty"` // the owner's own name for it
	State    string `json:"state"`
}

type tools struct{ d web.Deps }

// ToolFilter hides the admin tools from tools/list for anyone who is not a
// superadmin. It only hides: every admin tool checks its caller again when
// it runs.
func ToolFilter(d web.Deps) server.ToolFilterFunc {
	return func(ctx context.Context, list []mcpg.Tool) []mcpg.Tool {
		u, _ := auth.UserFrom(ctx)
		if d.CheckSuperadmin(u) == nil {
			return list
		}
		out := make([]mcpg.Tool, 0, len(list))
		for _, t := range list {
			if !strings.HasPrefix(t.Name, adminPrefix) {
				out = append(out, t)
			}
		}
		return out
	}
}

// Tools adds the WTF tools, all acting for the user the /mcp guard put on
// the request (an OAuth token's subject or an API key's owner).
func Tools(d web.Deps) func(s *server.MCPServer) {
	return func(s *server.MCPServer) {
		t := tools{d: d}
		t.addNumberTools(s)
		t.addAccountTools(s)
		t.addAdminTools(s)
	}
}

// ---------------------------------------------------------------- helpers

type hints struct{ readOnly, destructive, idempotent bool }

var (
	readOnly    = hints{readOnly: true, idempotent: true}
	change      = hints{idempotent: true}
	once        = hints{}
	destructive = hints{destructive: true, idempotent: true}
)

func newTool(name, title, desc string, h hints, opts ...mcpg.ToolOption) mcpg.Tool {
	base := []mcpg.ToolOption{
		mcpg.WithDescription(desc),
		mcpg.WithTitleAnnotation(title),
		mcpg.WithReadOnlyHintAnnotation(h.readOnly),
		mcpg.WithDestructiveHintAnnotation(h.destructive),
		mcpg.WithIdempotentHintAnnotation(h.idempotent),
		mcpg.WithOpenWorldHintAnnotation(false),
	}
	return mcpg.NewTool(name, append(base, opts...)...)
}

func deviceArg() mcpg.ToolOption {
	return mcpg.WithString("device_id", mcpg.Required(), mcpg.Description("Which of your numbers: its device_id from wtf_my_devices"))
}

func userArg() mcpg.ToolOption {
	return mcpg.WithString("user_id", mcpg.Required(), mcpg.Description("The account's user_id, from wtf_admin_list_users"))
}

// fail turns an action's error into a tool error: a refusal as it stands,
// "not found" for anything the caller cannot reach (another person's number
// reads exactly like one that does not exist), and nothing internal.
func fail(err error) *mcpg.CallToolResult {
	var r *web.Refusal
	switch {
	case errors.As(err, &r):
		return mcpg.NewToolResultError(r.Msg)
	case errors.Is(err, web.ErrNotSuperadmin):
		return mcpg.NewToolResultError(AdminOnlyMessage)
	case errors.Is(err, pkgError.ErrDeviceNotFound):
		return mcpg.NewToolResultError(deviceNotFoundMessage)
	case errors.Is(err, store.ErrNotFound):
		return mcpg.NewToolResultError(notFoundMessage)
	}
	var v pkgError.ValidationError
	if errors.As(err, &v) {
		return mcpg.NewToolResultError(v.Error())
	}
	logrus.WithError(err).Warn("[WTF] MCP tool failed")
	return mcpg.NewToolResultError(somethingWentWrongMessage)
}

func ok(v any, text string) (*mcpg.CallToolResult, error) {
	return mcpg.NewToolResultStructured(v, text), nil
}

// signedIn wraps a tool for any signed-in, active user (the guard already
// refused everyone else; this is the same check once more).
func signedIn(h func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error)) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		u, found := auth.UserFrom(ctx)
		if !found || u == nil || u.Status != store.StatusActive {
			return mcpg.NewToolResultError(notFoundMessage), nil
		}
		return h(ctx, u, req)
	}
}

// admin wraps an admin tool: CheckSuperadmin on the caller at call time,
// before anything is read; anyone else learns nothing but that.
func (t tools) admin(h func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error)) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		u, _ := auth.UserFrom(ctx)
		if err := t.d.CheckSuperadmin(u); err != nil {
			return mcpg.NewToolResultError(AdminOnlyMessage), nil
		}
		return h(ctx, u, req)
	}
}

// ------------------------------------------------------------- my numbers

func (t tools) addNumberTools(s *server.MCPServer) {
	s.AddTool(newTool(MyDevicesTool, "My WhatsApp numbers",
		"Lists the WhatsApp numbers linked to your account: each one's device_id, phone number, WhatsApp name, the label you gave it, and its state "+
			"(logged_in, disconnected or another state). When you have more than one number, pass the chosen device_id "+
			"as the device_id argument of the other WhatsApp tools; with exactly one, they use it by default.", readOnly),
		signedIn(func(ctx context.Context, _ *store.User, _ mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			list, err := t.d.Devices.ListDevices(ctx)
			if err != nil {
				return fail(err), nil
			}
			items := make([]myDevice, 0, len(list))
			for _, d := range list {
				items = append(items, myDevice{DeviceID: d.ID, Phone: d.PhoneNumber, Name: d.DisplayName, Label: d.Label, State: string(d.State)})
			}
			return ok(map[string]any{"total": len(items), "items": items}, fmt.Sprintf("%d WhatsApp number(s) linked", len(items)))
		}))

	s.AddTool(newTool(LinkNumberTool, "Link a WhatsApp number",
		"Starts linking a WhatsApp number to your account, as My WhatsApp > Link a number does. Without device_id it reserves a new slot "+
			"(within your number limit); with device_id it continues linking a slot that never finished. Give phone (international format) "+
			"to get an 8-character pairing code the person types in WhatsApp > Linked devices > Link with phone number; without phone you "+
			"get a QR code image to scan, valid for about a minute. Then call wtf_number_status until the state is connected.", once,
		mcpg.WithString("phone", mcpg.Description("The number to link, in international format (e.g. +27825550100): returns a pairing code instead of a QR code")),
		mcpg.WithString("device_id", mcpg.Description("An unfinished slot from wtf_number_status (state not_linked) to continue instead of reserving a new one")),
	), signedIn(t.linkNumber))

	s.AddTool(newTool(NumberStatusTool, "Number link status",
		"Says the state of your numbers as My WhatsApp shows them: connected, disconnected (linked, not online now), signed_out (the phone "+
			"signed this link out), refused (with the reason) or not_linked (never finished linking). With device_id, just that number.", readOnly,
		mcpg.WithString("device_id", mcpg.Description("One number's device_id; omit for all of them")),
	), signedIn(func(ctx context.Context, _ *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		list, err := t.d.MyNumbers(ctx)
		if err != nil {
			return fail(err), nil
		}
		if id := strings.TrimSpace(req.GetString("device_id", "")); id != "" {
			for _, n := range list {
				if n.DeviceID == id {
					return ok(n, n.DeviceID+": "+n.State)
				}
			}
			return fail(pkgError.ErrDeviceNotFound), nil
		}
		return ok(map[string]any{"total": len(list), "items": list}, fmt.Sprintf("%d number(s)", len(list)))
	}))

	s.AddTool(newTool(ReconnectNumberTool, "Reconnect a number",
		"Reconnects one of your linked numbers to WhatsApp, as Reconnect on its row does. It settles over a few seconds: check with wtf_number_status.", once, deviceArg()),
		signedIn(func(ctx context.Context, _ *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			id := req.GetString("device_id", "")
			if err := t.d.Devices.ReconnectDevice(ctx, id); err != nil {
				return fail(err), nil
			}
			return ok(map[string]any{"device_id": id, "reconnecting": true}, "Reconnecting "+id)
		}))

	s.AddTool(newTool(UnlinkNumberTool, "Unlink a number",
		"Unlinks one of your numbers from this server and deletes its messages and media here, as Unlink on its row does. The WhatsApp "+
			"account and the chats on the phone stay. This cannot be undone: linking again needs the phone.", destructive, deviceArg()),
		signedIn(func(ctx context.Context, _ *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			id := req.GetString("device_id", "")
			if err := t.d.Devices.RemoveDevice(ctx, id); err != nil {
				return fail(err), nil
			}
			return ok(map[string]any{"device_id": id, "unlinked": true}, "Unlinked "+id)
		}))

	s.AddTool(newTool(RenameNumberTool, "Rename a number",
		"Gives one of your numbers a name only you see (up to 40 characters), or removes the name when label is empty.", change, deviceArg(),
		mcpg.WithString("label", mcpg.Required(), mcpg.Description(`The name; "" removes it`)),
	), signedIn(func(ctx context.Context, _ *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		id := req.GetString("device_id", "")
		label, err := t.d.RenameNumber(ctx, id, req.GetString("label", ""))
		if err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"device_id": id, "label": label}, "Renamed "+id)
	}))

	s.AddTool(newTool(GetWebhookTool, "Get a number's webhook",
		"Shows where one of your numbers sends its events (the webhook address) and whether a signing secret is set. The secret itself is never shown.", readOnly, deviceArg()),
		signedIn(func(ctx context.Context, _ *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			w, err := t.d.NumberWebhook(ctx, req.GetString("device_id", ""))
			if err != nil {
				return fail(err), nil
			}
			return ok(w, "Webhook: "+orNone(w.URL))
		}))

	s.AddTool(newTool(SetWebhookTool, "Set a number's webhook",
		"Sends one of your number's events to url (public https only; the address is checked before it is saved). secret, when given, "+
			"replaces the signing secret; without it the current secret stays.", change, deviceArg(),
		mcpg.WithString("url", mcpg.Required(), mcpg.Description("https:// address that receives the events")),
		mcpg.WithString("secret", mcpg.Description("New signing secret (HMAC); omit to keep the current one")),
	), signedIn(func(ctx context.Context, _ *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		w, err := t.d.SaveNumberWebhook(ctx, req.GetString("device_id", ""), req.GetString("url", ""), req.GetString("secret", ""), false, "")
		if err != nil {
			return fail(err), nil
		}
		return ok(w, "Webhook saved")
	}))

	s.AddTool(newTool(RemoveWebhookTool, "Remove a number's webhook",
		"Stops sending one of your number's events anywhere and forgets its address and signing secret.", destructive, deviceArg()),
		signedIn(func(ctx context.Context, _ *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			w, err := t.d.SaveNumberWebhook(ctx, req.GetString("device_id", ""), "", "", true, "")
			if err != nil {
				return fail(err), nil
			}
			return ok(w, "Webhook removed")
		}))
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func (t tools) linkNumber(ctx context.Context, _ *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
	id := strings.TrimSpace(req.GetString("device_id", ""))
	if id != "" {
		// Continue an unfinished slot: the same owner check as every number route.
		if _, err := t.d.Devices.GetDevice(ctx, id); err != nil {
			return fail(err), nil
		}
	} else {
		newID, err := t.d.StartLinking(ctx)
		if err != nil {
			return fail(err), nil
		}
		id = newID
	}
	if phone := strings.TrimSpace(req.GetString("phone", "")); phone != "" {
		code, err := t.d.Devices.LoginDeviceWithCode(ctx, id, phone)
		if err != nil {
			if errors.Is(err, pkgError.ErrDeviceNotFound) {
				return fail(err), nil
			}
			return mcpg.NewToolResultError("We couldn't get a code for that number (device_id " + id + "). Check it and try again."), nil
		}
		return ok(map[string]any{"device_id": id, "pair_code": code, "phone": phone},
			"Pairing code "+code+": on the phone, WhatsApp > Linked devices > Link a device > Link with phone number instead, then type it. device_id "+id)
	}
	return t.qr(ctx, id)
}

func (t tools) qr(ctx context.Context, id string) (*mcpg.CallToolResult, error) {
	resp, err := t.d.Devices.LoginDevice(ctx, id)
	if errors.Is(err, pkgError.ErrAlreadyLoggedIn) {
		return mcpg.NewToolResultError("This number is already linked."), nil
	}
	if err != nil {
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fail(err), nil
		}
		logrus.WithError(err).Warnf("[WTF] MCP link %s", id)
		return mcpg.NewToolResultError("We couldn't start linking (device_id " + id + "). Try again with this device_id."), nil
	}
	text := fmt.Sprintf("Scan this QR code in WhatsApp > Linked devices > Link a device within %d seconds. device_id %s", int(resp.Duration.Seconds()), id)
	info := map[string]any{"device_id": id, "expires_in": int(resp.Duration.Seconds()), "qr_code": resp.Code}
	png, rerr := os.ReadFile(filepath.Clean(resp.ImagePath))
	if rerr != nil {
		return ok(info, text+" (QR image unavailable; qr_code holds its text)")
	}
	res := mcpg.NewToolResultImage(text, base64.StdEncoding.EncodeToString(png), "image/png")
	res.StructuredContent = info
	return res, nil
}

// ------------------------------------------------------------- my account

func (t tools) addAccountTools(s *server.MCPServer) {
	s.AddTool(newTool(MyAccountTool, "My account",
		"Your account on this server: phone number, status, whether you are an admin, how many numbers you may link and have, when you "+
			"joined and last signed in, and how long messages and media are kept here (the retention window).", readOnly),
		signedIn(func(ctx context.Context, _ *store.User, _ mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			p, err := t.d.MyProfile(ctx)
			if err != nil {
				return fail(err), nil
			}
			return ok(p, "Account "+p.Phone+": "+p.Retention)
		}))
	s.AddTool(newTool(MySessionsTool, "My browser sessions",
		"Lists where you are signed in in a browser (browser, started, last seen). Read-only: ending a session is done on the Account page.", readOnly),
		signedIn(func(ctx context.Context, _ *store.User, _ mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			list, err := t.d.MySessions(ctx)
			if err != nil {
				return fail(err), nil
			}
			return ok(map[string]any{"total": len(list), "items": list}, fmt.Sprintf("%d session(s)", len(list)))
		}))
	s.AddTool(newTool(MyConnectedAppsTool, "My connected apps",
		"Lists the apps (such as Claude) connected to your account over MCP, when each connected and was last used. Read-only: disconnecting is done on the Account page.", readOnly),
		signedIn(func(ctx context.Context, _ *store.User, _ mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			list, err := t.d.MyConnectedApps(ctx)
			if err != nil {
				return fail(err), nil
			}
			return ok(map[string]any{"total": len(list), "items": list}, fmt.Sprintf("%d connected app(s)", len(list)))
		}))
	s.AddTool(newTool(MyAPIKeysTool, "My API keys",
		"Lists your API keys by name and first characters, with when each was made and last used. Never the keys themselves. Read-only: making or revoking keys is done on the Account page.", readOnly),
		signedIn(func(ctx context.Context, _ *store.User, _ mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			list, err := t.d.MyAPIKeys(ctx)
			if err != nil {
				return fail(err), nil
			}
			return ok(map[string]any{"total": len(list), "items": list}, fmt.Sprintf("%d API key(s)", len(list)))
		}))
	s.AddTool(newTool(MyPasskeysTool, "My passkeys",
		"Lists your passkeys by name, with when each was added and last used. Read-only: adding or removing passkeys is done on the Account page.", readOnly),
		signedIn(func(ctx context.Context, _ *store.User, _ mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			list, err := t.d.MyPasskeys(ctx)
			if err != nil {
				return fail(err), nil
			}
			return ok(map[string]any{"total": len(list), "items": list}, fmt.Sprintf("%d passkey(s)", len(list)))
		}))
}

// ------------------------------------------------------------------- admin

const adminNote = " Superadmins only."

func (t tools) addAdminTools(s *server.MCPServer) {
	s.AddTool(newTool(AdminListUsersTool, "Users",
		"Lists accounts, 50 a page, newest first by default: each one's user_id, number, status, number limit, last sign-in and linked numbers. "+
			"search matches phone digits; status filters; sort is created, last_login or phone."+adminNote, readOnly,
		mcpg.WithString("search", mcpg.Description("Digits of the phone number to look for")),
		mcpg.WithString("status", mcpg.Enum("active", "disabled"), mcpg.Description("Only accounts in this status")),
		mcpg.WithString("sort", mcpg.Enum("created", "last_login", "phone"), mcpg.Description("Sort key (default: newest first)")),
		mcpg.WithBoolean("desc", mcpg.Description("Sort descending")),
		mcpg.WithNumber("page", mcpg.Min(1), mcpg.Description("Page number, from 1")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		page := req.GetInt("page", 1)
		total, items, err := t.d.AdminListUsers(ctx, u, web.UsersFilter{Search: req.GetString("search", ""), Status: req.GetString("status", ""),
			Sort: req.GetString("sort", ""), Desc: req.GetBool("desc", false), Page: page})
		if err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"total": total, "page": max(page, 1), "page_size": store.DefaultPageSize, "items": items},
			fmt.Sprintf("%d account(s) in all; %d on this page", total, len(items)))
	}))

	s.AddTool(newTool(AdminAddUserTool, "Add a user",
		"Creates an account for a phone number and returns a one-time sign-in link (valid 15 minutes) to pass to that person. "+
			"Use this when sign-ups are closed."+adminNote, once,
		mcpg.WithString("phone", mcpg.Required(), mcpg.Description("The person's full number with its country code, e.g. +27825550100")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		phone, link, err := t.d.AdminAddUser(ctx, u, req.GetString("phone", ""), "")
		if err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"phone": phone, "signin_link": link}, "Added "+phone+". Sign-in link (works once, 15 minutes): "+link)
	}))

	s.AddTool(newTool(AdminSigninLinkTool, "Get a sign-in link",
		"Makes a one-time sign-in link (valid 15 minutes) for an active account, to pass to that person."+adminNote, once, userArg()),
		t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			link, err := t.d.AdminSigninLink(ctx, u, req.GetString("user_id", ""))
			if err != nil {
				return fail(err), nil
			}
			return ok(map[string]any{"signin_link": link}, "Sign-in link (works once, 15 minutes): "+link)
		}))

	s.AddTool(newTool(AdminSetUserStatusTool, "Disable or enable a user",
		"Disables an account (ending its sessions, API keys and connected apps at once) or enables it again. With also_unlink, disabling "+
			"also unlinks every number the account holds. You cannot disable yourself."+adminNote, hints{destructive: true, idempotent: true}, userArg(),
		mcpg.WithString("status", mcpg.Required(), mcpg.Enum("active", "disabled"), mcpg.Description("active enables; disabled disables")),
		mcpg.WithBoolean("also_unlink", mcpg.Description("disabled only: also unlink their numbers (default false)")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		id, status := req.GetString("user_id", ""), req.GetString("status", "")
		if err := t.d.AdminSetStatus(ctx, u, id, status, req.GetBool("also_unlink", false), ""); err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"user_id": id, "status": status}, "Account "+id+" is now "+status)
	}))

	s.AddTool(newTool(AdminSetNumberLimitTool, "Set a user's number limit",
		"Sets how many WhatsApp numbers an account may link, 0 to 20. Numbers already linked stay."+adminNote, change, userArg(),
		mcpg.WithNumber("max_numbers", mcpg.Required(), mcpg.Min(0), mcpg.Max(store.MaxDevicesLimit), mcpg.Description("The new limit, 0 to 20")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		id, n := req.GetString("user_id", ""), req.GetInt("max_numbers", -1)
		if err := t.d.AdminSetLimit(ctx, u, id, n, ""); err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"user_id": id, "max_numbers": n}, fmt.Sprintf("Limit set to %d", n))
	}))

	s.AddTool(newTool(AdminChangeNumberTool, "Change a user's sign-in number",
		"Moves an account to another phone number: they sign in with the new one, and every session, API key, connected app and sign-in "+
			"link of the old one ends. Never your own account or an admin's."+adminNote, once, userArg(),
		mcpg.WithString("phone", mcpg.Required(), mcpg.Description("The new full number with its country code")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		id := req.GetString("user_id", "")
		phone, err := t.d.AdminChangePhone(ctx, u, id, req.GetString("phone", ""), "")
		if err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"user_id": id, "phone": phone}, "Account "+id+" now signs in with "+phone)
	}))

	s.AddTool(newTool(AdminUnlinkNumberTool, "Unlink a user's number",
		"Unlinks one of an account's WhatsApp numbers and deletes its messages and media here. Cannot be undone."+adminNote, destructive, userArg(),
		mcpg.WithString("device_id", mcpg.Required(), mcpg.Description("The number's device_id, from wtf_admin_list_users")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		id, dev := req.GetString("user_id", ""), req.GetString("device_id", "")
		if err := t.d.AdminUnlinkNumber(ctx, u, id, dev); err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"user_id": id, "device_id": dev, "unlinked": true}, "Unlinked "+dev)
	}))

	s.AddTool(newTool(AdminDeleteUserTool, "Delete a disabled user",
		"Deletes a disabled account for good: its sign-in, numbers (unlinked) and their messages and media here. Disable it first. "+
			"confirm must be the account's phone number. Never your own account, never the one holding the sign-in number. Cannot be undone."+adminNote,
		hints{destructive: true}, userArg(),
		mcpg.WithString("confirm", mcpg.Required(), mcpg.Description("The account's phone number, typed out, to confirm")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		id := req.GetString("user_id", "")
		if err := t.d.AdminDeleteUser(ctx, u, id, req.GetString("confirm", ""), ""); err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"user_id": id, "deleted": true}, "Account "+id+" deleted")
	}))

	s.AddTool(newTool(AdminActivityTool, "Activity",
		"The audit log in plain words, newest first, 50 a page; with user_id, only events about or by that account."+adminNote, readOnly,
		mcpg.WithString("user_id", mcpg.Description("Only this account's events")),
		mcpg.WithNumber("page", mcpg.Min(1), mcpg.Description("Page number, from 1")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		page := max(req.GetInt("page", 1), 1)
		total, items, err := t.d.AdminActivity(ctx, u, req.GetString("user_id", ""), page)
		if err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"total": total, "page": page, "page_size": 50, "items": items}, fmt.Sprintf("%d event(s) in all", total))
	}))

	s.AddTool(newTool(AdminWaitlistTool, "Waitlist",
		"Lists people waiting for an account (or, with status declined, those declined), 50 a page, oldest first, with the counts and how full the server is."+adminNote, readOnly,
		mcpg.WithString("status", mcpg.Enum("waiting", "declined"), mcpg.Description("Which list (default waiting)")),
		mcpg.WithNumber("page", mcpg.Min(1), mcpg.Description("Page number, from 1")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		w, err := t.d.AdminWaitlist(ctx, u, req.GetString("status", ""), req.GetInt("page", 1))
		if err != nil {
			return fail(err), nil
		}
		return ok(w, fmt.Sprintf("%d waiting, %d declined", w.Waiting, w.Declined))
	}))

	s.AddTool(newTool(AdminWaitlistDecideTool, "Decide on someone on the waitlist",
		"One person on the waitlist: admit lets them in (makes their account and sends the sign-in link from the sign-in number, or returns "+
			"it when sending fails); decline moves them to Declined; reopen moves a declined person back to waiting; remove deletes them from the list."+adminNote,
		hints{destructive: true},
		mcpg.WithString("phone", mcpg.Required(), mcpg.Description("Their number, as wtf_admin_waitlist lists it")),
		mcpg.WithString("action", mcpg.Required(), mcpg.Enum("admit", "decline", "reopen", "remove")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		phone, err := store.NormalizePhone(req.GetString("phone", ""))
		if err == nil {
			phone, err = web.ParseWaitlistNumber(store.DigitsOnly(phone))
		}
		if err != nil {
			return fail(store.ErrNotFound), nil
		}
		var out web.WaitlistOutcome
		if action := req.GetString("action", ""); action == "admit" {
			out, err = t.d.AdminAdmit(ctx, u, phone, "")
		} else {
			out, err = t.d.AdminDecideWaitlist(ctx, u, phone, action, "")
		}
		if err != nil {
			return fail(err), nil
		}
		if out.Result == "error" {
			return mcpg.NewToolResultError(out.Message), nil
		}
		text := out.Result
		if out.Link != "" {
			text += ": the link could not be sent on WhatsApp; pass this sign-in link on (works once, 15 minutes): " + out.Link
		} else if out.Message != "" {
			text += ": " + out.Message
		}
		return ok(out, text)
	}))

	s.AddTool(newTool(AdminSettingsTool, "Server settings",
		"Admin > Settings: which number sends sign-in links and whether it is connected, your own linked numbers that could do it, how full "+
			"the server is, and its memory and disk use."+adminNote, readOnly),
		t.admin(func(ctx context.Context, u *store.User, _ mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			s, err := t.d.AdminSettings(ctx, u)
			if err != nil {
				return fail(err), nil
			}
			return ok(s, "Sign-in number: "+s.SigninStatus)
		}))

	s.AddTool(newTool(AdminSetSigninNumberTool, "Set the sign-in number",
		"Chooses which of your own linked numbers sends sign-in links and waitlist messages (device_id from wtf_admin_settings choices)."+adminNote, change,
		mcpg.WithString("device_id", mcpg.Required(), mcpg.Description("One of your own linked numbers")),
	), t.admin(func(ctx context.Context, u *store.User, req mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
		id := req.GetString("device_id", "")
		if err := t.d.AdminSetSigninNumber(ctx, u, id, ""); err != nil {
			return fail(err), nil
		}
		return ok(map[string]any{"signin_device_id": id}, "Sign-in number set to "+id)
	}))

	s.AddTool(newTool(AdminCapacityTool, "Capacity",
		"How full the server is: numbers linked against the cap (max 0 means no cap), accounts, people waiting, and whether it is nearly full (90%)."+adminNote, readOnly),
		t.admin(func(ctx context.Context, u *store.User, _ mcpg.CallToolRequest) (*mcpg.CallToolResult, error) {
			c, near, err := t.d.AdminCapacity(ctx, u)
			if err != nil {
				return fail(err), nil
			}
			text := fmt.Sprintf("%d of %d numbers linked", c.Linked, c.Max)
			if c.Max == 0 {
				text = fmt.Sprintf("%d numbers linked (no cap)", c.Linked)
			}
			return ok(map[string]any{"linked": c.Linked, "max": c.Max, "users": c.Users, "waiting": c.Waiting, "near_full": near}, text)
		}))
}
