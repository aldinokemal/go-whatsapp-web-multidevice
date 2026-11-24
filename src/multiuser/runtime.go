package multiuser

import (
	"context"
	"errors"
	"net/url"
	"runtime/debug"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/account"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/capacity"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/files"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/mcpauth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/netguard"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/passkey"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/retention"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/signin"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/web"
	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types"
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
	// OAuth is the MCP authorization server (session-subject mode, CIMD
	// only); nil when MCP is off.
	OAuth *oauth.Server
	// MediaRoot is where downloads land, one directory per device, and
	// what /files serves from.
	MediaRoot string
	// Capacity answers how full the server is (WTF_MAX_PHONES).
	Capacity capacity.Service
	// Deleter deletes accounts (Delete my account); Start finishes any left
	// half done. Its Chats is set by Start.
	Deleter *account.Deleter
	// Sweeper frees link slots that were never scanned; Start runs it.
	Sweeper *owners.StaleSlotSweeper
	// RetentionRoots are the folders retention sweeps by file age (media
	// downloads and send items); HistoryDumpDir is where history sync
	// dumps land. Setup sets them from config; Start hands them to the
	// worker.
	RetentionRoots []string
	HistoryDumpDir string
	// Retention is the retention worker Start runs; nil while retention is
	// off (WTF_RETENTION_HOURS=0) or before Start.
	Retention *retention.Worker
	// ResumeDeletionsEvery is how often Start retries account deletions
	// left half done (default resumeDeletionsEvery).
	ResumeDeletionsEvery time.Duration

	stop context.CancelFunc // ends Start's background work
}

// CIMDMaxBytes caps a client ID metadata document, both in the outbound
// guard and in the OAuth server's own read (docs/mcp-oauth.md). Claude's
// documents are well under 1 KiB; 64 KiB leaves room for a client with many
// redirect URIs and still bounds what one anonymous request can make the
// server download.
const CIMDMaxBytes int64 = 64 << 10

// resumeDeletionsEvery is how often a deletion left half done (a WhatsApp
// error mid way) is retried while the server runs: hourly, like retention.
const resumeDeletionsEvery = time.Hour

// staleSweepEvery is how often the stale-slot sweeper looks.
const staleSweepEvery = 10 * time.Minute

// ChatData is the part of chat storage the runtime needs
// (chatstorage.IChatStorageRepository has it): clearing a deleted number, and
// the retention purges.
type ChatData interface {
	DeleteDeviceData(deviceID string) error
	retention.Chats
}

// Start begins the runtime's background work, once, after Install: it
// finishes every account deletion left half done (a crash, a WhatsApp error)
// at once and then hourly, runs the stale-slot sweeper and, while retention
// is on, the retention worker, until Close. chats clears a deleted number's chat storage even when
// the device manager no longer knows it, and is what retention purges; nil
// leaves the former to the device purge alone and retention to files and
// the accounts store.
func (r *Runtime) Start(chats ChatData) {
	if r.stop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.stop = cancel
	if chats != nil {
		r.Deleter.Chats = chats
	}
	go func() {
		defer func() {
			if p := recover(); p != nil {
				logrus.Errorf("[WTF] resuming account deletions: recovered from panic\n%s", debug.Stack())
			}
		}()
		every := r.ResumeDeletionsEvery
		if every <= 0 {
			every = resumeDeletionsEvery
		}
		r.resumeDeletions(ctx, every)
	}()
	go func() {
		defer func() {
			if p := recover(); p != nil {
				logrus.Errorf("[WTF] stale slot sweeper: recovered from panic\n%s", debug.Stack())
			}
		}()
		r.Sweeper.Run(ctx, staleSweepEvery)
	}()
	if window, on := r.Settings.RetentionWindow(); on {
		w := &retention.Worker{Window: window, Accounts: r.Store,
			FileRoots: r.RetentionRoots, HistoryDumpDir: r.HistoryDumpDir}
		if chats != nil {
			w.Chats = chats
		}
		r.Retention = w
		go func() {
			defer func() {
				if p := recover(); p != nil {
					logrus.Errorf("[WTF] retention worker: recovered from panic\n%s", debug.Stack())
				}
			}()
			w.Run(ctx)
		}()
	}
}

// resumeDeletions finishes every account left "deleting" at once and then
// every interval until ctx ends, so a deletion that stopped half way does
// not wait for a restart. One loop: a pass never overlaps the one before.
func (r *Runtime) resumeDeletions(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		if err := r.Deleter.ResumePending(ctx); err != nil && ctx.Err() == nil {
			logrus.WithError(err).Warn("[WTF] some account deletions are still unfinished; they are retried within the hour")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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

// liveSlots lists the device manager's slots with the JID each holds right
// now ("" while unpaired), for the stale-slot sweeper.
func liveSlots(dm *whatsapp.DeviceManager) func() []owners.PairedDevice {
	return func() []owners.PairedDevice {
		var out []owners.PairedDevice
		for _, inst := range dm.ListDevices() {
			jid := inst.JID()
			if client := inst.GetClient(); jid == "" && client != nil && client.Store != nil && client.Store.ID != nil {
				jid = client.Store.ID.String()
			}
			out = append(out, owners.PairedDevice{ID: inst.ID(), JID: jid})
		}
		return out
	}
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

// recordSlotEvent keeps accounts.db's memory of a slot current: who it is
// while connected, and that the phone signed it out (the upstream handler then
// clears the slot's WhatsApp identity, so this is the last chance to read it).
// The hook runs before upstream handles the event.
func recordSlotEvent(ctx context.Context, st *store.Store, inst *whatsapp.DeviceInstance, evt any) {
	if inst == nil {
		return
	}
	switch evt.(type) {
	case *events.Connected, *events.PushNameSetting:
		phone, name := slotIdentity(inst)
		if phone != "" {
			_ = st.RecordIdentity(ctx, inst.ID(), phone, name)
		}
	case *events.LoggedOut:
		phone, name := slotIdentity(inst)
		_ = st.MarkSignedOut(ctx, inst.ID(), phone, name)
	}
}

// slotIdentity reads the number (bare digits) and name a slot holds, from the
// instance's cached state or, before that is filled, its WhatsApp store.
func slotIdentity(inst *whatsapp.DeviceInstance) (phone, name string) {
	phone = strings.TrimPrefix(inst.PhoneNumber(), "+")
	name = inst.DisplayName()
	if client := inst.GetClient(); client != nil && client.Store != nil {
		if phone == "" && client.Store.ID != nil && client.Store.ID.Server == types.DefaultUserServer {
			phone = client.Store.ID.User
		}
		if name == "" {
			name = client.Store.PushName
		}
	}
	return phone, name
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
	capSvc := capacity.Service{Store: st, MaxPhones: s.MaxPhones}
	// The server-wide cap is read on every add, in the same statement that
	// reserves the slot (store.ReserveOwner).
	devices := owners.NewScopedDevicesCapped(inner, st, func() int { return s.MaxPhones })

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
		ServiceDeviceID: serviceID, Now: func() time.Time { return time.Now().UTC() },
		// Phase 4: an unknown number that may not sign up now (sign-up closed,
		// or open but full) joins the waitlist. Sign-up itself stays as
		// WTF_SIGNUP_OPEN says (closed unless set).
		Waitlist: st, Full: capSvc.Full, DefaultMaxDevices: s.DefaultMaxDevices,
	}
	pairs := dmPairs{dm: dm}
	// sendFromService sends text from the service number to phone (E.164),
	// into the chat that person opened with it.
	sendFromService := func(ctx context.Context, phone, text string) error {
		inst, ok := dm.GetDevice(serviceID(ctx))
		if !ok || !inst.IsLoggedIn() {
			return errors.New("the sign-in number isn't connected")
		}
		to := types.NewJID(strings.TrimPrefix(phone, "+"), types.DefaultUserServer)
		return signin.ClientSender{}.SendText(ctx, inst, to, text)
	}

	middleware.DeviceResolveFunc = func(c fiber.Ctx, id string) (*whatsapp.DeviceInstance, string, error) {
		return resolver.Resolve(c.Context(), id)
	}
	whatsapp.EventHook = func(ctx context.Context, inst *whatsapp.DeviceInstance, evt any) {
		sh.OnEvent(ctx, inst, evt)
		recordSlotEvent(ctx, st, inst, evt)
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
				} else {
					// Accepted: forget any earlier refusal and remember who this is.
					bg := context.Background()
					_ = st.ClearRefusal(bg, id)
					phone, name := slotIdentity(inst)
					_ = st.RecordIdentity(bg, id, phone, name)
				}
			}(inst.ID())
		}
	}

	// Account deletion purges through the inner (unscoped) usecase: the
	// account is already refused by the scoped one once it is "deleting".
	// Retention: history sync stores nothing already past the window. The
	// worker itself starts in Start.
	retentionHours := 0
	if window, on := s.RetentionWindow(); on {
		retentionHours = s.RetentionHours
		whatsapp.HistoryCutoffHook = func() (time.Time, bool) {
			return time.Now().UTC().Add(-window), true
		}
	}

	deleter := &account.Deleter{Store: st, Devices: inner, MediaRoot: config.PathMedia, ServiceDevice: serviceID}
	sweeper := &owners.StaleSlotSweeper{Store: st, Devices: inner, Live: liveSlots(dm)}
	rt := &Runtime{Settings: s, Store: st, Auth: am, Resolver: resolver, Devices: devices, Signin: sh,
		MediaRoot: config.PathMedia, Capacity: capSvc, Deleter: deleter, Sweeper: sweeper,
		RetentionRoots: []string{config.PathMedia, config.PathSendItems}, HistoryDumpDir: config.PathStorages,
		Web: web.Deps{Store: st, Auth: am, Passkeys: pk, Devices: devices, ProductName: s.ProductName,
			PublicURL: s.PublicURL, ServicePhone: servicePhone, Paired: pairs.Paired, StorageDir: storageDir,
			DeleteAccount: deleter.Delete, SendWhatsApp: sendFromService, Capacity: capSvc,
			DefaultMaxDevices: s.DefaultMaxDevices, RetentionHours: retentionHours}}

	if s.MCPEnabled {
		authn := &mcpauth.Authenticator{Auth: am, Store: st}
		// The client_id is attacker-chosen: fetch it only through the
		// outbound guard, never following a redirect.
		cimdClient := netguard.NewClient(netguard.Options{Timeout: 10 * time.Second, MaxRedirects: 0, MaxBody: CIMDMaxBytes})
		if s.CIMDHTTPClient != nil {
			cimdClient = s.CIMDHTTPClient
		}
		srv, err := oauth.New(oauth.Config{
			IssuerURL:   s.PublicURL,
			ResourceURL: s.MCPResourceURL(),
			StorageURI:  config.McpOAuthDBURI,
			Session:     authn,
			DisableDCR:  true,
			CIMD: &oauth.CIMDConfig{
				AllowedHosts: s.MCPClientHosts,
				HTTPClient:   cimdClient,
				MaxBytes:     CIMDMaxBytes,
				UserAgent:    userAgentToken(s.ProductName) + "-connector-check/1",
			},
		}, nil)
		if err != nil {
			middleware.DeviceResolveFunc = nil
			whatsapp.EventHook = nil
			whatsapp.HistoryCutoffHook = nil
			st.Close()
			return nil, err
		}
		rt.OAuth = srv
		rt.Web.Grants = srv
		deleter.Grants = srv
		rt.Web.MCPURL = s.MCPResourceURL()
		authn.Web = rt.Web
	}

	// Downloads land under <media root>/<device id>/ and are fetched, owner
	// checked, from /files/<device id>/...
	usecase.MediaRootHook = files.MediaRoot(rt.MediaRoot, s.PublicURL)
	return rt, nil
}

// userAgentToken keeps the letters, digits and hyphens of name, so the
// product name can start an HTTP User-Agent.
func userAgentToken(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 128 && (r == '-' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9')) {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "WTF"
	}
	return b.String()
}

// Install mounts everything multi-user mode serves, in this order (the
// route stack is walked in registration order, so the order is the policy):
//
//  1. the session middleware (loads the cookie's user for every request);
//  2. WTF's own pages, each with its own page gate;
//  3. when MCP is on: the OAuth endpoints that the OAuth and MCP specs make
//     public (discovery documents, token, authorize, which sends anonymous
//     browsers to sign in), then /mcp behind its own guard (an OAuth token or
//     an API key; a 401 with the Bearer challenge otherwise). Neither may sit
//     behind the gates below: a connector has no session cookie;
//  4. the API gate chain: an API key, else the cookie, then same-origin and
//     CSRF for cookie sessions. Everything registered after this point (the
//     upstream REST API, /files) is behind it;
//  5. /files, owner-checked media.
//
// mcpDeps are the usecases the MCP tools call; Install adds the owner-checked
// device resolver and the my-devices tool.
func (r *Runtime) Install(app *fiber.App, dm *whatsapp.DeviceManager, mcpDeps uimcp.Deps) {
	app.Use(r.Auth.Middleware())
	web.Mount(app, r.Web)
	web.MountDeviceLabelPage(app, r.Web)
	if r.OAuth != nil {
		r.OAuth.RegisterPublic(app)
		app.Use("/mcp", mcpauth.Middleware(r.OAuth, r.Auth, r.Store))
		mcpDeps.ResolveDevice = r.Resolver.Resolve
		mcpDeps.ExtraTools = mcpauth.Tools(r.Web)
		mcpDeps.ToolFilter = mcpauth.ToolFilter(r.Web)
		uimcp.Register(app, dm, mcpDeps)
	}
	// A browser asking for a page nothing serves gets the 404 page here,
	// before the API gate would answer it in JSON.
	app.Use(web.NotFoundPages(app, r.Web))
	// A key request carries no session id, so RequireSameOriginAPI and
	// VerifyCSRF (which judge cookie sessions only) let it through.
	app.Use(r.Auth.APIKey(), r.Auth.RequireAPI(), r.Auth.RequireSameOriginAPI(), r.Auth.VerifyCSRF())
	// PUT /devices/:device_id/label: behind the API gate (a session or an
	// API key), before the upstream /devices routes.
	web.MountDeviceLabelAPI(app, r.Web)
	files.Mount(app, files.Deps{Resolver: r.Resolver, Root: r.MediaRoot})
}

// Close releases the global hooks, the OAuth store and the accounts store.
// Safe to call once, typically deferred right after Setup succeeds.
func (r *Runtime) Close() error {
	if r.stop != nil {
		r.stop()
	}
	middleware.DeviceResolveFunc = nil
	whatsapp.EventHook = nil
	whatsapp.HistoryCutoffHook = nil
	usecase.MediaRootHook = nil
	if r.OAuth != nil {
		_ = r.OAuth.Close()
	}
	return r.Store.Close()
}
