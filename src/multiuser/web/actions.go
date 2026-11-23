package web

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"time"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/account"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/capacity"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/sirupsen/logrus"
)

// Actions shared by the pages and the MCP tools (house rule S5: whatever the
// GUI can do, MCP can do, through the same code). Each one takes a context
// carrying the signed-in user (auth.WithUser) or the acting admin, does the
// work, audits it, and returns plain data. A *Refusal is a refusal a person
// (or an AI caller) is shown as it stands; store.ErrNotFound and
// pkgError.ErrDeviceNotFound mean "no such thing you can reach". Every admin
// action checks CheckSuperadmin itself, whatever gate sits in front of it.

// Refusal is an action refused for a reason worth saying, in plain words.
type Refusal struct{ Msg string }

func (r *Refusal) Error() string { return r.Msg }

func refuse(msg string) error { return &Refusal{Msg: msg} }

// ErrNotSuperadmin: the caller is not a superadmin (or not active).
var ErrNotSuperadmin = errors.New("not available")

// CheckSuperadmin is the one superadmin gate the admin actions and the MCP
// admin tools share. It reads the env allowlist through auth.Manager, never
// the database, and refuses anyone not active.
func (d Deps) CheckSuperadmin(u *store.User) error {
	if u == nil || !d.Auth.IsSuperadmin(u) {
		return ErrNotSuperadmin
	}
	return nil
}

// DeviceLimitMessage is what a person sees at their linked-number limit.
const DeviceLimitMessage = deviceLimitMessage

// ---------------------------------------------------------------- my numbers

// NumberInfo is one of the caller's WhatsApp numbers and its state:
// connected, disconnected, signed_out, refused or not_linked.
type NumberInfo struct {
	DeviceID string    `json:"device_id"`
	State    string    `json:"state"`
	Phone    string    `json:"phone,omitempty"`
	Name     string    `json:"name,omitempty"`
	Label    string    `json:"label,omitempty"`
	Added    time.Time `json:"added"`
	Refusal  string    `json:"refusal,omitempty"`
}

func numberInfos(devs []domainDevice.Device, slots map[string]store.Slot) []NumberInfo {
	labels := make(map[string]string, len(devs))
	for _, dv := range devs {
		labels[dv.ID] = dv.Label
	}
	views := numberViews(devs, slots)
	out := make([]NumberInfo, 0, len(views))
	for _, v := range views {
		n := NumberInfo{DeviceID: v.ID, State: v.State, Phone: v.Phone, Name: v.Name, Label: labels[v.ID], Added: v.Added}
		if v.State == numRefused {
			n.Refusal = v.RefusalText()
		}
		out = append(out, n)
	}
	return out
}

// MyNumbers lists the signed-in user's numbers with the state My WhatsApp
// shows for each, through the owner-scoped device usecase.
func (d Deps) MyNumbers(ctx context.Context) ([]NumberInfo, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, pkgError.ErrDeviceNotFound
	}
	list, err := d.Devices.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	slots, err := d.Store.SlotsOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return numberInfos(list, slots), nil
}

// StartLinking reserves a new number slot for the signed-in user (Link a
// number), within their limit and the server's cap.
func (d Deps) StartLinking(ctx context.Context) (string, error) {
	dev, err := d.Devices.AddDevice(ctx, "", nil)
	if err != nil {
		if errors.Is(err, owners.ErrDeviceLimitReached) {
			return "", refuse(deviceLimitMessage)
		}
		var v pkgError.ValidationError
		if errors.As(err, &v) {
			return "", refuse(v.Error())
		}
		return "", err
	}
	return dev.ID, nil
}

// RenameNumber names one of the signed-in user's numbers; "" removes the name.
func (d Deps) RenameNumber(ctx context.Context, deviceID, label string) (string, error) {
	saved, err := owners.SetDeviceLabel(ctx, d.Store, deviceID, label)
	var v pkgError.ValidationError
	if errors.As(err, &v) {
		return "", refuse(labelErrorText(v))
	}
	return saved, err
}

// WebhookInfo is a number's webhook as shown: the address and whether a
// signing secret is set. The secret itself is write-only.
type WebhookInfo struct {
	URL       string `json:"url"`
	HasSecret bool   `json:"has_secret"`
}

func webhookInfo(cfg *chatstorage.DeviceWebhookConfig) WebhookInfo {
	var w WebhookInfo
	if cfg != nil {
		if cfg.WebhookURL != nil {
			w.URL = *cfg.WebhookURL
		}
		w.HasSecret = cfg.WebhookSecret != ""
	}
	return w
}

// NumberWebhook reads one of the signed-in user's numbers' webhook.
func (d Deps) NumberWebhook(ctx context.Context, deviceID string) (WebhookInfo, error) {
	cfg, err := d.Devices.GetDeviceWebhookConfig(ctx, deviceID)
	if err != nil {
		return WebhookInfo{}, err
	}
	return webhookInfo(cfg), nil
}

// SaveNumberWebhook sets (url; a non-empty secret replaces the current one,
// "" keeps it) or, with remove, clears a number's webhook. Every address goes
// through the owner-scoped usecase's guard (public https only, TLS verified)
// before it is saved, and the change is audited.
func (d Deps) SaveNumberWebhook(ctx context.Context, deviceID, url, secret string, remove bool, ip string) (WebhookInfo, error) {
	cur, err := d.Devices.GetDeviceWebhookConfig(ctx, deviceID)
	if err != nil {
		return WebhookInfo{}, err
	}
	if cur == nil {
		cur = &chatstorage.DeviceWebhookConfig{}
	}
	next := &chatstorage.DeviceWebhookConfig{WebhookEvents: cur.WebhookEvents, WebhookSecret: cur.WebhookSecret}
	action := "webhook_set"
	if remove {
		empty := ""
		next.WebhookURL, next.WebhookSecret, action = &empty, "", "webhook_cleared"
	} else {
		url = strings.TrimSpace(url)
		if url == "" {
			return WebhookInfo{}, refuse("Enter the address to send events to.")
		}
		next.WebhookURL = &url
		if secret != "" {
			next.WebhookSecret = secret
		}
	}
	if err := d.Devices.SetDeviceWebhookConfig(ctx, deviceID, next); err != nil {
		var v pkgError.ValidationError
		if errors.As(err, &v) {
			return WebhookInfo{}, refuse(webhookErrorText(v.Error()))
		}
		return WebhookInfo{}, err
	}
	if u, ok := auth.UserFrom(ctx); ok {
		_ = d.Store.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: action, DeviceID: deviceID, IP: ip})
	}
	return webhookInfo(next), nil
}

// ------------------------------------------------------------- my account

// SessionInfo is one of the user's browser sessions as Account lists it.
type SessionInfo struct {
	Device   string     `json:"device"`
	Started  time.Time  `json:"started"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
	Current  bool       `json:"current"`
}

// MySessions lists the signed-in user's browser sessions (never their ids).
func (d Deps) MySessions(ctx context.Context) ([]SessionInfo, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, store.ErrNotFound
	}
	views, err := sessionList(ctx, d, u.ID, "")
	if err != nil {
		return nil, err
	}
	out := make([]SessionInfo, 0, len(views))
	for _, v := range views {
		out = append(out, SessionInfo{Device: v.Device, Started: v.Started, LastSeen: v.LastSeen, Current: v.Current})
	}
	return out, nil
}

// AppInfo is one connected app (an MCP OAuth grant).
type AppInfo struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	Connected string `json:"connected"`
	LastUsed  string `json:"last_used"`
}

// MyConnectedApps lists the signed-in user's connected apps; nil when MCP is off.
func (d Deps) MyConnectedApps(ctx context.Context) ([]AppInfo, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, store.ErrNotFound
	}
	if d.Grants == nil {
		return []AppInfo{}, nil
	}
	grants, err := d.Grants.ListGrants(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	out := []AppInfo{}
	for _, g := range grantViews(grants) {
		out = append(out, AppInfo{Name: g.Name, Host: g.Host, Connected: g.Connected, LastUsed: g.LastUsed})
	}
	return out, nil
}

// KeyInfo is one API key as listed: its name and the characters that tell
// keys apart, never the key.
type KeyInfo struct {
	Name     string `json:"name"`
	Shown    string `json:"shown"`
	Added    string `json:"added"`
	LastUsed string `json:"last_used"`
}

// MyAPIKeys lists the signed-in user's API keys (read-only).
func (d Deps) MyAPIKeys(ctx context.Context) ([]KeyInfo, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, store.ErrNotFound
	}
	list, err := d.Store.APIKeysOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	out := []KeyInfo{}
	for _, k := range apiKeyViews(list) {
		out = append(out, KeyInfo{Name: k.Name, Shown: k.Shown, Added: k.Added, LastUsed: k.LastUsed})
	}
	return out, nil
}

// PasskeyInfo is one passkey as listed (never the credential).
type PasskeyInfo struct {
	Name     string `json:"name"`
	Added    string `json:"added"`
	LastUsed string `json:"last_used"`
}

// MyPasskeys lists the signed-in user's passkeys (read-only).
func (d Deps) MyPasskeys(ctx context.Context) ([]PasskeyInfo, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, store.ErrNotFound
	}
	list, err := d.Store.PasskeysOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	out := []PasskeyInfo{}
	for _, p := range passkeyViews(list) {
		out = append(out, PasskeyInfo{Name: p.Name, Added: p.Added, LastUsed: p.LastUsed})
	}
	return out, nil
}

// Profile is the signed-in user's own account.
type Profile struct {
	UserID         string     `json:"user_id"`
	Phone          string     `json:"phone"`
	Status         string     `json:"status"`
	Superadmin     bool       `json:"superadmin"`
	MaxNumbers     int        `json:"max_numbers"` // 0: no personal limit
	Numbers        int        `json:"numbers"`
	Added          time.Time  `json:"added"`
	LastSignIn     *time.Time `json:"last_sign_in,omitempty"`
	RetentionHours int        `json:"retention_hours"` // 0: messages are kept
	Retention      string     `json:"retention"`
	MCPURL         string     `json:"mcp_url,omitempty"`
}

// MyProfile is the signed-in user's account, as fresh as the store has it.
func (d Deps) MyProfile(ctx context.Context) (Profile, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return Profile{}, store.ErrNotFound
	}
	ids, err := d.Store.DevicesOf(ctx, u.ID)
	if err != nil {
		return Profile{}, err
	}
	note := retentionNote(d.RetentionHours)
	if note == "" {
		note = "Messages and media are kept: retention is off on this server."
	}
	return Profile{UserID: u.ID, Phone: u.Phone, Status: u.Status, Superadmin: d.Auth.IsSuperadmin(u), MaxNumbers: u.MaxDevices,
		Numbers: len(ids), Added: u.CreatedAt, LastSignIn: u.LastLoginAt, RetentionHours: d.RetentionHours, Retention: note, MCPURL: d.MCPURL}, nil
}

// ------------------------------------------------------------------- admin

// UsersFilter is the Users list's search, filter, sort and page.
type UsersFilter struct {
	Search string // phone digits
	Status string // "", active, disabled
	Sort   string // "", created, last_login, phone
	Desc   bool
	Page   int // from 1
}

// UserSummary is one account as an admin sees it on Users.
type UserSummary struct {
	UserID     string       `json:"user_id"`
	Phone      string       `json:"phone"`
	Status     string       `json:"status"`
	Superadmin bool         `json:"superadmin"`
	Self       bool         `json:"self"`
	MaxNumbers int          `json:"max_numbers"`
	Added      time.Time    `json:"added"`
	LastSignIn string       `json:"last_sign_in"`
	Numbers    []NumberInfo `json:"numbers"`
}

// AdminListUsers is one page of Users (50 a page).
func (d Deps) AdminListUsers(ctx context.Context, caller *store.User, f UsersFilter) (int, []UserSummary, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return 0, nil, err
	}
	q := usersQuery{Q: strings.TrimSpace(f.Search), Status: f.Status, Sort: f.Sort, Desc: f.Desc, Page: f.Page}.clean()
	total, users, rows, err := usersPage(ctx, d, q, caller)
	if err != nil {
		return 0, nil, err
	}
	out := make([]UserSummary, 0, len(rows))
	for i, r := range rows {
		s := UserSummary{UserID: r.ID, Phone: r.Phone, Status: users[i].Status, Superadmin: r.IsAdmin, Self: r.IsSelf,
			MaxNumbers: r.MaxDevices, Added: r.Added, LastSignIn: r.LastSignIn, Numbers: []NumberInfo{}}
		for _, n := range r.Numbers {
			ni := NumberInfo{DeviceID: n.ID, State: n.State, Phone: n.Phone, Name: n.Name, Added: n.Added}
			if n.State == numRefused {
				ni.Refusal = n.RefusalText()
			}
			s.Numbers = append(s.Numbers, ni)
		}
		out = append(out, s)
	}
	return total, out, nil
}

// signinLink mints a one-time sign-in link for phone, the same link the
// "Get sign-in link" button and the admin-link command hand out.
func (d Deps) signinLink(ctx context.Context, phone string) (string, error) {
	return signinLinkFor(ctx, d, phone)
}

// AdminAddUser creates an account for typed (any spelling of a full number)
// and returns its number and a one-time sign-in link.
func (d Deps) AdminAddUser(ctx context.Context, caller *store.User, typed, ip string) (string, string, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return "", "", err
	}
	phone, err := store.NormalizePhone(typed)
	if err != nil {
		return "", "", refuse("Enter a full number with its country code, like +27 82 555 0100.")
	}
	if _, err := d.Store.UserByPhone(ctx, phone); err == nil {
		return "", "", refuse("That number already has an account. Use Get sign-in link on its row instead.")
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", "", err
	}
	var u *store.User
	if d.DefaultMaxDevices > 0 {
		u, err = d.Store.CreateUserWithLimit(ctx, phone, d.DefaultMaxDevices)
	} else {
		u, err = d.Store.CreateUser(ctx, phone)
	}
	if err != nil {
		return "", "", err
	}
	_ = d.Store.Audit(ctx, store.AuditEvent{UserID: u.ID, ActorID: caller.ID, Action: "user_added", IP: ip})
	link, err := d.signinLink(ctx, phone)
	if err != nil {
		return "", "", err
	}
	return phone, link, nil
}

// AdminSigninLink mints a one-time sign-in link for an active account.
func (d Deps) AdminSigninLink(ctx context.Context, caller *store.User, targetID string) (string, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return "", err
	}
	target, err := d.Store.UserByID(ctx, targetID)
	if err != nil {
		return "", err
	}
	if target.Status != store.StatusActive {
		return "", refuse("Enable this account first, then get a sign-in link.")
	}
	return d.signinLink(ctx, target.Phone)
}

// AdminSetStatus enables or disables an account. Disabling ends every way
// back in (sessions, API keys, connected apps) and, with unlink, unlinks
// every number the account holds.
func (d Deps) AdminSetStatus(ctx context.Context, caller *store.User, targetID, status string, unlink bool, ip string) error {
	if err := d.CheckSuperadmin(caller); err != nil {
		return err
	}
	if status != store.StatusActive && status != store.StatusDisabled {
		return refuse("That's not a status we know.")
	}
	if status == store.StatusDisabled && targetID == caller.ID {
		return refuse("You can't disable your own account.")
	}
	if err := d.Store.SetUserStatus(ctx, targetID, status); err != nil {
		if errors.Is(err, store.ErrDeleting) {
			return refuse("This account is being deleted.")
		}
		return err
	}
	if status != store.StatusDisabled {
		_ = d.Store.Audit(ctx, store.AuditEvent{UserID: targetID, ActorID: caller.ID, Action: "user_enabled", IP: ip})
		return nil
	}
	if err := revokeCredentials(ctx, d, targetID); err != nil {
		return err
	}
	_ = d.Store.Audit(ctx, store.AuditEvent{UserID: targetID, ActorID: caller.ID, Action: "user_disabled", IP: ip})
	if unlink {
		target, err := d.Store.UserByID(ctx, targetID)
		if err != nil {
			return err
		}
		if _, err := owners.RemoveAllDevicesAsAdmin(ctx, d.Devices, d.Store, caller, target); err != nil {
			return err
		}
	}
	return nil
}

// AdminUnlinkNumber unlinks one of an account's numbers, through the same
// owner-checked RemoveDevice the person's own Unlink uses.
func (d Deps) AdminUnlinkNumber(ctx context.Context, caller *store.User, targetID, deviceID string) error {
	if err := d.CheckSuperadmin(caller); err != nil {
		return err
	}
	target, err := d.Store.UserByID(ctx, targetID)
	if err != nil {
		return err
	}
	return owners.RemoveDeviceAsAdmin(ctx, d.Devices, caller, target, deviceID)
}

// AdminSetLimit sets how many numbers an account may link (0 to 20).
func (d Deps) AdminSetLimit(ctx context.Context, caller *store.User, targetID string, n int, ip string) error {
	if err := d.CheckSuperadmin(caller); err != nil {
		return err
	}
	target, err := d.Store.UserByID(ctx, targetID)
	if err != nil {
		return err
	}
	if n < 0 || n > store.MaxDevicesLimit {
		return refuse("Choose a number from 0 to " + strconv.Itoa(store.MaxDevicesLimit) + ".")
	}
	if err := d.Store.SetMaxDevices(ctx, target.ID, n); err != nil {
		return err
	}
	_ = d.Store.Audit(ctx, store.AuditEvent{UserID: target.ID, ActorID: caller.ID, Action: "user_limit_set", Detail: strconv.Itoa(n), IP: ip})
	return nil
}

// AdminChangePhone moves an account to another number. It ends every way
// back in, and every sign-in link minted for the old number. An admin's
// number (decided by the server's settings) never moves either way.
func (d Deps) AdminChangePhone(ctx context.Context, caller *store.User, targetID, typed, ip string) (string, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return "", err
	}
	target, err := d.Store.UserByID(ctx, targetID)
	if err != nil {
		return "", err
	}
	if target.ID == caller.ID {
		return "", refuse("You can't change your own number here.")
	}
	if d.Auth.IsSuperadminPhone(target.Phone) {
		return "", refuse("An admin's number is set in the server's settings.")
	}
	phone, err := store.NormalizePhone(typed)
	if err != nil {
		return "", refuse("Enter the full number with its country code.")
	}
	if d.Auth.IsSuperadminPhone(phone) {
		return "", refuse("That number belongs to an admin.")
	}
	if phone == target.Phone {
		return phone, nil
	}
	old, err := d.Store.SetUserPhone(ctx, target.ID, phone)
	if errors.Is(err, store.ErrPhoneTaken) {
		return "", refuse("That number already belongs to another user.")
	}
	if err != nil {
		return "", err
	}
	if err := revokeCredentials(ctx, d, target.ID); err != nil {
		return "", err
	}
	if err := d.Store.DeleteLoginTokensFor(ctx, old); err != nil {
		return "", err
	}
	_ = d.Store.Audit(ctx, store.AuditEvent{UserID: target.ID, ActorID: caller.ID, Action: "user_phone_changed", Detail: old + " " + phone, IP: ip})
	return phone, nil
}

// AdminDeleteUser deletes a disabled account (never the caller's own, never
// the sign-in number's owner) with the same Deleter as Delete my account.
// confirm must be the account's own number (any spelling).
func (d Deps) AdminDeleteUser(ctx context.Context, caller *store.User, targetID, confirm, ip string) error {
	if err := d.CheckSuperadmin(caller); err != nil {
		return err
	}
	if d.DeleteAccount == nil {
		return refuse("Deleting accounts is off on this server.")
	}
	target, err := d.Store.UserByID(ctx, targetID)
	if err != nil {
		return err
	}
	if target.ID == caller.ID {
		return refuse("You can't delete your own account here.")
	}
	if target.Status == store.StatusActive {
		return refuse("Disable this account first.")
	}
	v, err := deleteAccountData(ctx, d, target)
	if err != nil {
		return err
	}
	if v != nil && v.OwnsService {
		return refuse("This account has the sign-in number. Choose another one in Settings first.")
	}
	typed := store.DigitsOnly(strings.TrimSpace(confirm))
	if len(typed) < 8 || typed != store.DigitsOnly(target.Phone) {
		return refuse("Type their number to confirm.")
	}
	if err := d.DeleteAccount(ctx, target.ID); err != nil {
		if errors.Is(err, account.ErrOwnsServiceNumber) {
			return refuse("This account has the sign-in number. Choose another one in Settings first.")
		}
		// Stopped half way: the account is already refused everywhere and the
		// retry finishes it. Anything earlier changed nothing.
		if cur, gerr := d.Store.UserByID(ctx, target.ID); gerr == nil && cur.Status != store.StatusDeleting {
			logrus.WithError(err).Warnf("[WTF] admin delete account %s", target.ID)
			return refuse("We couldn't delete this account. Try again.")
		}
		logrus.WithError(err).Warnf("[WTF] admin delete account %s: left to finish later", target.ID)
	}
	_ = d.Store.Audit(ctx, store.AuditEvent{UserID: target.ID, ActorID: caller.ID, Action: "user_deleted", IP: ip})
	return nil
}

// ActivityItem is one audit event in plain words.
type ActivityItem struct {
	At       time.Time `json:"at"`
	Text     string    `json:"text"`
	Action   string    `json:"action"`
	UserID   string    `json:"user_id,omitempty"`
	ActorID  string    `json:"actor_id,omitempty"`
	DeviceID string    `json:"device_id,omitempty"`
}

// AdminActivity is one page (50) of the audit log, newest first, optionally
// about one user.
func (d Deps) AdminActivity(ctx context.Context, caller *store.User, userID string, page int) (int, []ActivityItem, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return 0, nil, err
	}
	if userID != "" {
		if _, err := d.Store.UserByID(ctx, userID); err != nil {
			return 0, nil, err
		}
	}
	events, total, rows, err := activityPage(ctx, d.Store, userID, page)
	if err != nil {
		return 0, nil, err
	}
	out := make([]ActivityItem, 0, len(rows))
	for i, r := range rows {
		var b strings.Builder
		for _, p := range r.Parts {
			b.WriteString(p.Text)
		}
		e := events[i]
		out = append(out, ActivityItem{At: r.At, Text: b.String(), Action: e.Action, UserID: e.UserID, ActorID: e.ActorID, DeviceID: e.DeviceID})
	}
	return total, out, nil
}

// WaitlistItem is one person on the waitlist.
type WaitlistItem struct {
	Phone      string     `json:"phone"`
	Position   int        `json:"position,omitempty"`
	Status     string     `json:"status"`
	Joined     time.Time  `json:"joined"`
	AskedAgain *time.Time `json:"asked_again,omitempty"`
}

// WaitlistPage is one page (50) of the waitlist.
type WaitlistPage struct {
	Status   string             `json:"status"`
	Total    int                `json:"total"`
	Waiting  int                `json:"waiting"`
	Declined int                `json:"declined"`
	Full     bool               `json:"full"`
	Capacity *capacity.Capacity `json:"capacity,omitempty"`
	Items    []WaitlistItem     `json:"items"`
}

// AdminWaitlist is one page of Waiting (or, with status declined, Declined).
func (d Deps) AdminWaitlist(ctx context.Context, caller *store.User, status string, page int) (WaitlistPage, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return WaitlistPage{}, err
	}
	w, err := loadWaitlist(ctx, d, status, page)
	if err != nil {
		return WaitlistPage{}, err
	}
	out := WaitlistPage{Status: w.Status, Total: w.Total, Waiting: w.Waiting, Declined: w.Declined, Full: w.Full, Capacity: w.Capacity, Items: []WaitlistItem{}}
	for _, r := range waitlistRows(w.Items, w.Offset, w.Status == store.WaitlistWaiting, !w.Full) {
		out.Items = append(out.Items, WaitlistItem{Phone: r.Phone, Position: r.Position, Status: r.Status, Joined: r.Joined, AskedAgain: r.AskedAgain})
	}
	return out, nil
}

// WaitlistOutcome says what a waitlist action did: admitted, sent-failed
// (Link is the sign-in link to pass on), exists, declined, reopened, removed
// or error (Message says why).
type WaitlistOutcome struct {
	Result  string `json:"result"`
	Message string `json:"message,omitempty"`
	Link    string `json:"link,omitempty"`
}

// ParseWaitlistNumber reads a waitlist key: the number's 8 to 15 digits.
func ParseWaitlistNumber(digits string) (string, error) {
	if digits == "" || store.DigitsOnly(digits) != digits {
		return "", store.ErrNotFound
	}
	phone, err := store.NormalizePhone("+" + digits)
	if err != nil || store.DigitsOnly(phone) != digits {
		return "", store.ErrNotFound
	}
	return phone, nil
}

// AdminAdmit lets one person in from the waitlist: their account is made
// and the sign-in link is sent from the sign-in number (or handed back).
func (d Deps) AdminAdmit(ctx context.Context, caller *store.User, phone, ip string) (WaitlistOutcome, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return WaitlistOutcome{}, err
	}
	if d.Capacity != nil && d.Capacity.Full(ctx) {
		return WaitlistOutcome{Result: "error", Message: "The server is full. Free a number or raise the limit first."}, nil
	}
	limit := d.DefaultMaxDevices
	if limit < 1 {
		limit = 2
	}
	u, created, err := d.Store.AdmitFromWaitlist(ctx, phone, caller.ID, limit)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return WaitlistOutcome{Result: "error", Message: "This number isn't on the waitlist any more."}, nil
	case errors.Is(err, store.ErrInvalid):
		return WaitlistOutcome{Result: "error", Message: "This number was declined. Move it back to waiting first."}, nil
	case err != nil:
		return WaitlistOutcome{}, err
	}
	if !created {
		return WaitlistOutcome{Result: "exists", Message: "This number already has an account. Nothing was sent."}, nil
	}
	_ = d.Store.Audit(ctx, store.AuditEvent{UserID: u.ID, ActorID: caller.ID, Action: "waitlist_admitted", IP: ip})
	link, err := d.signinLink(ctx, phone)
	if err != nil {
		return WaitlistOutcome{}, err
	}
	text := "You're in. Here's your " + d.ProductName + " sign-in link. It works once, for 15 minutes:\n\n" + link
	if d.SendWhatsApp == nil {
		return WaitlistOutcome{Result: "sent-failed", Link: link}, nil
	}
	if err := d.SendWhatsApp(ctx, phone, text); err != nil {
		logrus.WithError(err).Warn("[WTF] waitlist: could not send the sign-in link")
		return WaitlistOutcome{Result: "sent-failed", Link: link}, nil
	}
	return WaitlistOutcome{Result: "admitted"}, nil
}

// AdminDecideWaitlist declines, reopens (moves back to waiting) or removes
// one person on the waitlist.
func (d Deps) AdminDecideWaitlist(ctx context.Context, caller *store.User, phone, action, ip string) (WaitlistOutcome, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return WaitlistOutcome{}, err
	}
	var err error
	var out WaitlistOutcome
	switch action {
	case "decline":
		err, out.Result = d.Store.DeclineWaitlist(ctx, phone, caller.ID), "declined"
	case "reopen":
		err, out.Result = d.Store.ReopenWaitlist(ctx, phone), "reopened"
	case "remove":
		err, out.Result = d.Store.RemoveFromWaitlist(ctx, phone), "removed"
	default:
		return WaitlistOutcome{}, refuse("That's not a waitlist action we know.")
	}
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalid) {
		return WaitlistOutcome{Result: "error", Message: "This number has moved on. Reload the page."}, nil
	}
	if err != nil {
		return WaitlistOutcome{}, err
	}
	_ = d.Store.Audit(ctx, store.AuditEvent{ActorID: caller.ID, Action: map[string]string{
		"decline": "waitlist_declined", "reopen": "waitlist_reopened", "remove": "waitlist_removed"}[action], IP: ip})
	return out, nil
}

// SettingsInfo is Admin > Settings: the sign-in number and the server.
type SettingsInfo struct {
	SigninDeviceID string            `json:"signin_device_id"`
	SigninStatus   string            `json:"signin_status"` // Not set, Connected, Not connected
	Choices        []NumberInfo      `json:"choices"`       // the caller's own linked numbers
	Capacity       capacity.Capacity `json:"capacity"`
	NearFull       bool              `json:"near_full"`
	AllocMB        uint64            `json:"alloc_mb"`
	SysMB          uint64            `json:"sys_mb"`
	Goroutines     int               `json:"goroutines"`
	DiskMB         int64             `json:"disk_mb"`
}

// AdminSettings reads Settings.
func (d Deps) AdminSettings(ctx context.Context, caller *store.User) (SettingsInfo, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return SettingsInfo{}, err
	}
	s, err := settingsData(ctx, d)
	if err != nil {
		return SettingsInfo{}, err
	}
	out := SettingsInfo{SigninDeviceID: s.ServiceDeviceID, SigninStatus: s.ServiceStatus, Capacity: s.Capacity, NearFull: s.NearFull,
		AllocMB: s.AllocMB, SysMB: s.SysMB, Goroutines: s.Goroutines, DiskMB: s.DiskMB, Choices: []NumberInfo{}}
	for _, dv := range s.Devices {
		out.Choices = append(out.Choices, NumberInfo{DeviceID: dv.ID, State: "linked", Phone: dv.PhoneNumber, Name: dv.DisplayName, Label: dv.Label, Added: dv.CreatedAt})
	}
	return out, nil
}

// AdminCapacity is how full the server is.
func (d Deps) AdminCapacity(ctx context.Context, caller *store.User) (capacity.Capacity, bool, error) {
	if err := d.CheckSuperadmin(caller); err != nil {
		return capacity.Capacity{}, false, err
	}
	snap, err := d.capacityReader().Snapshot(ctx)
	if err != nil {
		return capacity.Capacity{}, false, err
	}
	return snap, nearFull(snap), nil
}

// AdminSetSigninNumber makes one of the caller's own linked numbers the one
// that sends sign-in links.
func (d Deps) AdminSetSigninNumber(ctx context.Context, caller *store.User, deviceID, ip string) error {
	if err := d.CheckSuperadmin(caller); err != nil {
		return err
	}
	ids, err := d.Store.DevicesOf(ctx, caller.ID)
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
		return refuse("Pick one of your own linked numbers.")
	}
	if err := d.Store.SetSetting(ctx, store.SettingServiceDeviceID, deviceID, caller.ID); err != nil {
		return err
	}
	_ = d.Store.Audit(ctx, store.AuditEvent{UserID: caller.ID, Action: "service_number_set", DeviceID: deviceID, IP: ip})
	return nil
}

// memStats is the server's memory, for Settings.
func memStats() (alloc, sys uint64, goroutines int) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return mem.Alloc / bytesPerMB, mem.Sys / bytesPerMB, runtime.NumGoroutine()
}
