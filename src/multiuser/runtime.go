package multiuser

import (
	"context"
	"net/url"
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
				time.Sleep(2 * time.Second) // let the store JID settle
				if refused, err := owners.GuardDuplicatePhone(context.Background(), pairs, st, id); refused {
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
