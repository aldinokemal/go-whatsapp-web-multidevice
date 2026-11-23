package multiuser

import (
	"context"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/passkey"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/signin"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/web"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types/events"
)

// Runtime is everything Setup built: the store, the auth/owner/signin/passkey
// layers and the web mount, plus the device usecase decorator that the REST
// API should use in place of the upstream one.
type Runtime struct {
	Settings Settings
	Store    *store.Store
	Auth     *auth.Manager
	Resolver *owners.Resolver
	Devices  domainDevice.IDeviceUsecase
	Signin   *signin.Handler
	Web      web.Deps
}

// guardDuplicatePhoneForTest, when set, stands in for
// owners.GuardDuplicatePhone inside Setup's post-pair goroutine. It exists
// only so a test can force a panic (or any other behaviour) at the exact
// point production code calls the real guard, without needing to engineer a
// genuine pairing race; production code never sets it.
var guardDuplicatePhoneForTest func(ctx context.Context, src owners.PairSource, st owners.OwnerLookup, deviceID string) (bool, error)

func runGuardDuplicatePhone(ctx context.Context, src owners.PairSource, st owners.OwnerLookup, deviceID string) (bool, error) {
	if guardDuplicatePhoneForTest != nil {
		return guardDuplicatePhoneForTest(ctx, src, st, deviceID)
	}
	return owners.GuardDuplicatePhone(ctx, src, st, deviceID)
}

// pairSettleCeiling is the most the post-pair goroutine will ever wait for
// the store JID to settle before running the duplicate guard regardless -
// the same ceiling the flat sleep this replaced used to enforce
// unconditionally. pairSettlePollInterval is how often it checks in the
// meantime.
const (
	pairSettleCeiling      = 2 * time.Second
	pairSettlePollInterval = 100 * time.Millisecond
)

// waitForPhoneNumberOrDeadline polls src.Paired() for deviceID's phone number
// to become readable (owners.PhoneNumberReady), checking every interval, and
// returns as soon as it does - or once deadline has elapsed, whichever comes
// first. It never waits longer than deadline: the deadline is tracked with
// its own timer rather than by summing poll intervals, so a slow poll
// interval can't push the total past it.
func waitForPhoneNumberOrDeadline(src owners.PairSource, deviceID string, deadline, interval time.Duration) {
	if phoneNumberSettled(src, deviceID) {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	timeout := time.NewTimer(deadline)
	defer timeout.Stop()
	for {
		select {
		case <-ticker.C:
			if phoneNumberSettled(src, deviceID) {
				return
			}
		case <-timeout.C:
			return
		}
	}
}

func phoneNumberSettled(src owners.PairSource, deviceID string) bool {
	for _, d := range src.Paired() {
		if d.ID == deviceID {
			return owners.PhoneNumberReady(d.JID)
		}
	}
	return false
}

// dmPairs adapts *whatsapp.DeviceManager to owners.PairSource.
type dmPairs struct{ dm *whatsapp.DeviceManager }

func (p dmPairs) Paired() []owners.PairedDevice {
	var out []owners.PairedDevice
	for _, inst := range p.dm.ListDevices() {
		out = append(out, owners.PairedDevice{ID: inst.ID(), JID: inst.JID()})
	}
	return out
}

func (p dmPairs) Logout(ctx context.Context, id string) error {
	return p.dm.LogoutDeviceKeepSlot(ctx, id)
}

// Setup opens the accounts store and builds every multi-user layer, then
// installs the two hooks (device resolution, WhatsApp event handling) that
// let unchanged upstream code become owner-scoped and the sign-in flow work.
func Setup(s Settings, dm *whatsapp.DeviceManager, inner domainDevice.IDeviceUsecase, storageDir string) (*Runtime, error) {
	st, err := store.Open(s.DBURI)
	if err != nil {
		return nil, err
	}
	pub, err := url.Parse(s.PublicURL)
	if err != nil {
		st.Close()
		return nil, err
	}
	secure := pub.Scheme == "https"
	am := auth.New(auth.Config{Secret: s.SessionSecret, Superadmins: s.Superadmins, SessionTTL: 30 * 24 * time.Hour, Secure: secure}, st)
	pk, err := passkey.New(pub.Hostname(), s.PublicURL, s.ProductName, st)
	if err != nil {
		st.Close()
		return nil, err
	}
	resolver := owners.NewResolver(dm, st)
	devices := owners.NewScopedDevices(inner, st)

	serviceID := func(ctx context.Context) string { v, _, _ := st.Setting(ctx, store.SettingServiceDeviceID); return v }
	servicePhone := func(ctx context.Context) string {
		if inst, ok := dm.GetDevice(serviceID(ctx)); ok && inst.IsLoggedIn() {
			return strings.TrimPrefix(inst.PhoneNumber(), "+")
		}
		return ""
	}
	sh := &signin.Handler{
		Store: st, Send: signin.ClientSender{}, ProductName: s.ProductName, PublicURL: s.PublicURL,
		SignupOpen: s.SignupOpen, IsSuperadmin: func(p string) bool { return IsSuperadmin(s, p) },
		Limiter: signin.NewLimiter(3, time.Hour), ResolvePN: signin.DefaultResolvePN,
		ServiceDeviceID: serviceID, Now: time.Now,
	}
	pairs := dmPairs{dm: dm}

	middleware.DeviceResolveFunc = func(c fiber.Ctx, id string) (*whatsapp.DeviceInstance, string, error) {
		return resolver.Resolve(c.Context(), id)
	}
	whatsapp.EventHook = func(ctx context.Context, inst *whatsapp.DeviceInstance, evt any) {
		sh.OnEvent(ctx, inst, evt)
		if _, ok := evt.(*events.PairSuccess); ok && inst != nil {
			go func(id string) {
				// recover is only ever able to catch a panic raised in the
				// same goroutine it runs in - a deferred recover in the
				// EventHook closure above this one would NOT catch a panic
				// here, so without this, a panic anywhere in the guard's
				// call chain would crash the whole process.
				defer func() {
					if r := recover(); r != nil {
						logrus.Errorf("[WTF] duplicate guard: recovered from panic on %s\n%s", id, debug.Stack())
					}
				}()
				// Let the store JID settle, but don't always wait the full
				// ceiling: poll for it instead of a flat sleep, which was a
				// worst-case 2-second duplicate-phone exposure window even
				// in the common case where the number is readable almost
				// immediately.
				waitForPhoneNumberOrDeadline(pairs, id, pairSettleCeiling, pairSettlePollInterval)
				if refused, err := runGuardDuplicatePhone(context.Background(), pairs, st, id); refused {
					logrus.Warnf("[WTF] refused duplicate link on %s", id)
				} else if err != nil {
					logrus.WithError(err).Warn("[WTF] duplicate guard failed")
				}
			}(inst.ID())
		}
	}

	return &Runtime{Settings: s, Store: st, Auth: am, Resolver: resolver, Devices: devices, Signin: sh,
		Web: web.Deps{Store: st, Auth: am, Passkeys: pk, Devices: devices, ProductName: s.ProductName,
			PublicURL: s.PublicURL, ServicePhone: servicePhone, Paired: pairs.Paired, StorageDir: storageDir}}, nil
}

// Install mounts the multi-user session middleware and every WTF page, then
// gates everything registered after it (the upstream REST API) behind
// sign-in, the same-origin check and CSRF.
func (r *Runtime) Install(app *fiber.App) {
	app.Use(r.Auth.Middleware())
	web.Mount(app, r.Web)
	app.Use(r.Auth.RequireAPI(), r.Auth.RequireSameOriginAPI(), r.Auth.VerifyCSRF())
}

// Close releases the two global hooks and closes the accounts store. Safe to
// call once, typically deferred right after Setup succeeds.
func (r *Runtime) Close() error {
	middleware.DeviceResolveFunc = nil
	whatsapp.EventHook = nil
	return r.Store.Close()
}
