package rest

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/fiber/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// routeAuthRepo is a storage fake with several device configs, enough for the
// client registry, route resolution, delivery, and the delete path.
type routeAuthRepo struct {
	domainChatStorage.IChatStorageRepository
	mu      sync.Mutex
	configs []*domainChatStorage.ChatwootDeviceConfig
	// links are returned by conversation (route resolution) and by Chatwoot
	// message id (delete path).
	links  []*domainChatStorage.ChatwootMessageLink
	stored []*domainChatStorage.ChatwootMessageLink
	// lookupErr, when set, fails device config lookups by identifier.
	lookupErr error
}

func (r *routeAuthRepo) failLookups(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookupErr = err
}

func (r *routeAuthRepo) GetChatwootDeviceConfigByIdentifier(identifier string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	r.mu.Lock()
	err := r.lookupErr
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	for _, cfg := range r.configs {
		if identifier != "" && (identifier == cfg.DeviceID || identifier == cfg.DeviceJID) {
			return cfg, nil
		}
	}
	return nil, nil
}

func (r *routeAuthRepo) GetChatwootDeviceConfigByInbox(accountID, inboxID int) (*domainChatStorage.ChatwootDeviceConfig, error) {
	for _, cfg := range r.configs {
		if cfg.AccountID == accountID && cfg.InboxID == inboxID {
			return cfg, nil
		}
	}
	return nil, nil
}

func (r *routeAuthRepo) ListChatwootDeviceConfigs() ([]*domainChatStorage.ChatwootDeviceConfig, error) {
	return r.configs, nil
}

func (r *routeAuthRepo) CountChatwootDeviceConfigs() (int, error) {
	return len(r.configs), nil
}

func (r *routeAuthRepo) GetLatestChatwootMessageLinkByConversation(conversationID, accountID int, _ bool, configID int64) (*domainChatStorage.ChatwootMessageLink, error) {
	for _, link := range r.links {
		if link.ChatwootConversationID == conversationID && link.ChatwootAccountID == accountID &&
			(configID == 0 || link.ChatwootConfigID == configID) {
			cloned := *link
			return &cloned, nil
		}
	}
	return nil, nil
}

func (r *routeAuthRepo) GetChatwootMessageLinkByChatwootID(deviceID string, chatwootMessageID int) (*domainChatStorage.ChatwootMessageLink, error) {
	for _, link := range r.links {
		if link.DeviceID == deviceID && link.ChatwootMessageID == chatwootMessageID {
			cloned := *link
			return &cloned, nil
		}
	}
	return nil, nil
}

func (r *routeAuthRepo) UpsertChatwootMessageLink(link *domainChatStorage.ChatwootMessageLink) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stored = append(r.stored, link)
	return nil
}

// routeAuthSend records every text send and the device it would go out from.
type routeAuthSend struct {
	domainSend.ISendUsecase
	mu    sync.Mutex
	sends []string // "<device alias> -> <phone>"
}

func (s *routeAuthSend) SendText(ctx context.Context, request domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	device := "<no device>"
	if instance, ok := whatsapp.DeviceFromContext(ctx); ok && instance != nil {
		device = instance.ID()
	}
	s.sends = append(s.sends, device+" -> "+request.Phone)
	return domainSend.GenericResponse{MessageID: fmt.Sprintf("WA-%d", len(s.sends)), Status: "ok"}, nil
}

func (s *routeAuthSend) taken() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	sends := s.sends
	s.sends = nil
	return sends
}

// routeAuthMessage records revokes.
type routeAuthMessage struct {
	domainMessage.IMessageUsecase
	revoked []string
}

func (m *routeAuthMessage) RevokeMessage(ctx context.Context, request domainMessage.RevokeRequest) (domainMessage.GenericResponse, error) {
	device := "<no device>"
	if instance, ok := whatsapp.DeviceFromContext(ctx); ok && instance != nil {
		device = instance.ID()
	}
	m.revoked = append(m.revoked, device+" revokes "+request.MessageID)
	return domainMessage.GenericResponse{}, nil
}

const (
	routeAuthSalesSecret   = "sales-channel-secret"
	routeAuthSupportSecret = "support-channel-secret"
	routeAuthSalesJID      = "628111111111@s.whatsapp.net"
	routeAuthSupportJID    = "628222222222@s.whatsapp.net"
	routeAuthOpenJID       = "628333333333@s.whatsapp.net"
)

type routeAuthFixture struct {
	app     *fiber.App
	repo    *routeAuthRepo
	send    *routeAuthSend
	message *routeAuthMessage
}

// newRouteAuthFixture wires both webhook endpoints to three paired devices on
// account 1: "sales" (inbox 2) and "support" (inbox 3) each have their own
// webhook secret, "open" (inbox 4) has none. The "sales" config has no stored
// JID, as when it was saved before the device was paired.
func newRouteAuthFixture(t *testing.T, globalSecret string) *routeAuthFixture {
	t.Helper()
	withChatwootWebhookSecret(t, globalSecret)
	chatwootReplays.reset()
	t.Cleanup(chatwootReplays.reset)

	repo := &routeAuthRepo{configs: []*domainChatStorage.ChatwootDeviceConfig{
		{ID: 1, DeviceID: "sales", ChatwootURL: "https://chat.example.com", AccountID: 1, InboxID: 2, APIToken: "t", Enabled: true, WebhookSecret: routeAuthSalesSecret},
		{ID: 2, DeviceID: "support", DeviceJID: routeAuthSupportJID, ChatwootURL: "https://chat.example.com", AccountID: 1, InboxID: 3, APIToken: "t", Enabled: true, WebhookSecret: routeAuthSupportSecret},
		{ID: 3, DeviceID: "open", DeviceJID: routeAuthOpenJID, ChatwootURL: "https://chat.example.com", AccountID: 1, InboxID: 4, APIToken: "t", Enabled: true},
	}}
	chatwoot.InitClientRegistry(repo)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	for alias, jid := range map[string]string{"sales": routeAuthSalesJID, "support": routeAuthSupportJID, "open": routeAuthOpenJID} {
		parsed, err := types.ParseJID(jid)
		if err != nil {
			t.Fatalf("parse %s: %v", jid, err)
		}
		dm.AddDevice(whatsapp.NewDeviceInstance(alias, &whatsmeow.Client{Store: &store.Device{ID: &parsed}}, nil))
	}

	f := &routeAuthFixture{repo: repo, send: &routeAuthSend{}, message: &routeAuthMessage{}}
	handler := &ChatwootHandler{SendUsecase: f.send, MessageUsecase: f.message, DeviceManager: dm, ChatStorageRepo: repo}
	f.app = fiber.New()
	f.app.Use("/chatwoot/webhook", ChatwootWebhookReplayGuard())
	f.app.Post("/chatwoot/webhook", handler.HandleWebhook)
	f.app.Post("/chatwoot/webhook/:device_id", handler.HandleDeviceWebhook)
	return f
}

// routeAuthMessageID makes every reply body (and so every signature) distinct.
var routeAuthMessageID = 1000

func agentReplyBody(inboxID, conversationID int) string {
	routeAuthMessageID++
	return fmt.Sprintf(`{"event":"message_created","message_type":"outgoing","id":%d,"content":"hi","account":{"id":1},`+
		`"conversation":{"id":%d,"inbox_id":%d,"meta":{"sender":{"phone_number":"+628999999999"}}}}`,
		routeAuthMessageID, conversationID, inboxID)
}

func chatwootSigned(secret, body string) map[string]string {
	if secret == "" {
		return nil
	}
	timestamp := nowUnix(0)
	return map[string]string{chatwootTimestampHeader: timestamp, chatwootSignatureHeader: chatwootSign(secret, timestamp, body)}
}

type routeAuthCase struct {
	name     string
	path     string
	inbox    int
	conv     int
	signWith string
	want     int
	wantSend string // "" = nothing may be sent
}

func runRouteAuthCases(t *testing.T, f *routeAuthFixture, cases []routeAuthCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			body := agentReplyBody(tt.inbox, tt.conv)
			got := postChatwootWebhook(t, f.app, webhookAuthRequest{path: tt.path, body: body, headers: chatwootSigned(tt.signWith, body)})
			sends := f.send.taken()
			if got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
			switch {
			case tt.wantSend == "" && len(sends) != 0:
				t.Errorf("sent %v, want nothing sent", sends)
			case tt.wantSend != "" && (len(sends) != 1 || sends[0] != tt.wantSend):
				t.Errorf("sent %v, want exactly [%s]", sends, tt.wantSend)
			}
		})
	}
}

// TestChatwootDeviceWebhookSignedReplyIsDelivered drives a signed agent reply
// through the per-device endpoint to the send usecase: the alias and JID path
// forms resolve to the same device, a wrong signature sends nothing, and one
// device's secret cannot deliver through another device.
func TestChatwootDeviceWebhookSignedReplyIsDelivered(t *testing.T) {
	for _, global := range []string{"", "global-secret"} {
		t.Run("global="+global, func(t *testing.T) {
			f := newRouteAuthFixture(t, global)
			salesByJID := "/chatwoot/webhook/" + url.PathEscape(routeAuthSalesJID)
			const toContact = " -> 628999999999"
			cases := []routeAuthCase{
				{name: "alias path, device secret", path: "/chatwoot/webhook/sales", inbox: 2, signWith: routeAuthSalesSecret, want: 200, wantSend: "sales" + toContact},
				{name: "JID path, device secret", path: salesByJID, inbox: 2, signWith: routeAuthSalesSecret, want: 200, wantSend: "sales" + toContact},
				{name: "alias path, unsigned", path: "/chatwoot/webhook/sales", inbox: 2, want: 401},
				{name: "alias path, wrong secret", path: "/chatwoot/webhook/sales", inbox: 2, signWith: "guess", want: 401},
				{name: "JID path, wrong secret", path: salesByJID, inbox: 2, signWith: "guess", want: 401},
				{name: "sales secret on the support endpoint", path: "/chatwoot/webhook/support", inbox: 3, signWith: routeAuthSalesSecret, want: 401},
				{name: "sales secret on the sales endpoint, support's inbox", path: "/chatwoot/webhook/sales", inbox: 3, signWith: routeAuthSalesSecret, want: 401},
				{name: "support secret delivers through support", path: "/chatwoot/webhook/support", inbox: 3, signWith: routeAuthSupportSecret, want: 200, wantSend: "support" + toContact},
			}
			if global != "" {
				cases = append(cases,
					routeAuthCase{name: "global secret does not open a device that has its own", path: "/chatwoot/webhook/sales", inbox: 2, signWith: global, want: 401},
					routeAuthCase{name: "device without its own secret uses the global one", path: "/chatwoot/webhook/open", inbox: 4, signWith: global, want: 200, wantSend: "open" + toContact},
					routeAuthCase{name: "device without its own secret refuses a device secret", path: "/chatwoot/webhook/open", inbox: 4, signWith: routeAuthSalesSecret, want: 401},
				)
			}
			runRouteAuthCases(t, f, cases)
		})
	}
}

// TestChatwootSharedWebhookEnforcesRoutedDeviceSecret: the shared endpoint
// routes by payload, so the secret that protects a delivery is the secret of
// the device it is routed to. Before this was enforced, an unsigned reply for
// a protected device's inbox was sent when CHATWOOT_WEBHOOK_SECRET was empty,
// and the global secret opened every device.
func TestChatwootSharedWebhookEnforcesRoutedDeviceSecret(t *testing.T) {
	const toContact = " -> 628999999999"

	t.Run("no global secret", func(t *testing.T) {
		f := newRouteAuthFixture(t, "")
		runRouteAuthCases(t, f, []routeAuthCase{
			{name: "unsigned reply for a protected device", inbox: 2, want: 401},
			{name: "wrong secret for a protected device", inbox: 2, signWith: "guess", want: 401},
			{name: "another device's secret", inbox: 3, signWith: routeAuthSalesSecret, want: 401},
			{name: "the routed device's secret", inbox: 2, signWith: routeAuthSalesSecret, want: 200, wantSend: "sales" + toContact},
			// Same answer as a wrong secret, so the status does not reveal
			// which inboxes are mapped to a protected device.
			{name: "unsigned reply for an unknown inbox", inbox: 99, want: 401},
			// An empty effective secret is rejected unless the unsigned opt-in is set.
			{name: "unsigned reply for a device without any secret", inbox: 4, want: 401},
		})
	})

	t.Run("with a global secret", func(t *testing.T) {
		const global = "global-secret"
		f := newRouteAuthFixture(t, global)
		// Conversation 7 is linked to the support device (by storage JID) under
		// support's configuration. Only a payload for support's inbox uses the
		// link; another inbox's configuration routes its own payload and does
		// not take the link's device or recipient. Conversation 6 has only a
		// legacy config-0 (env-era) link, which per-device mode never uses.
		f.repo.links = []*domainChatStorage.ChatwootMessageLink{{
			DeviceID: routeAuthSupportJID, WhatsAppChatJID: "628777777777@s.whatsapp.net",
			ChatwootConversationID: 7, ChatwootAccountID: 1, ChatwootConfigID: 2,
		}, {
			DeviceID: routeAuthSalesJID, WhatsAppChatJID: "628666666666@s.whatsapp.net",
			ChatwootConversationID: 6, ChatwootAccountID: 1,
		}}
		runRouteAuthCases(t, f, []routeAuthCase{
			{name: "global secret for a protected device", inbox: 2, signWith: global, want: 401},
			{name: "unsigned", inbox: 2, want: 401},
			{name: "another device's secret", inbox: 3, signWith: routeAuthSalesSecret, want: 401},
			{name: "the routed device's secret", inbox: 2, signWith: routeAuthSalesSecret, want: 200, wantSend: "sales" + toContact},
			{name: "global secret for a device without its own", inbox: 4, signWith: global, want: 200, wantSend: "open" + toContact},
			{name: "device secret for a device without its own", inbox: 4, signWith: routeAuthSalesSecret, want: 401},
			{name: "global secret, unknown inbox: acknowledged, nothing sent", inbox: 99, signWith: global, want: 200},
			{name: "unsigned, unknown inbox", inbox: 99, want: 401},
			{name: "device secret, unknown inbox", inbox: 99, signWith: routeAuthSalesSecret, want: 401},
			{name: "linked conversation from another inbox, global secret", inbox: 4, conv: 7, signWith: global, want: 200, wantSend: "open" + toContact},
			{name: "linked conversation from another inbox, that inbox's secret", inbox: 2, conv: 7, signWith: routeAuthSalesSecret, want: 200, wantSend: "sales" + toContact},
			{name: "linked conversation, the linked device's secret", inbox: 3, conv: 7, signWith: routeAuthSupportSecret, want: 200, wantSend: "support -> 628777777777"},
			{name: "legacy config-0 link is not used", inbox: 2, conv: 6, signWith: routeAuthSalesSecret, want: 200, wantSend: "sales" + toContact},
		})
	})

	// The sales config has no stored JID. A route that names the device by
	// its storage JID (as conversation links do) must still find its secret.
	t.Run("route by JID finds a config stored under the alias", func(t *testing.T) {
		f := newRouteAuthFixture(t, "")
		f.repo.links = []*domainChatStorage.ChatwootMessageLink{{
			DeviceID: routeAuthSalesJID, WhatsAppChatJID: "628999999999@s.whatsapp.net",
			ChatwootConversationID: 8, ChatwootAccountID: 1, ChatwootConfigID: 1,
		}}
		runRouteAuthCases(t, f, []routeAuthCase{
			{name: "unsigned", inbox: 2, conv: 8, want: 401},
			{name: "device secret", inbox: 2, conv: 8, signWith: routeAuthSalesSecret, want: 200, wantSend: "sales" + toContact},
		})
	})

	// Events nothing is done for still require a secret. The explicit opt-in
	// acknowledges them again, but a device that has its own secret stays closed.
	t.Run("ignored events without a global secret are rejected", func(t *testing.T) {
		f := newRouteAuthFixture(t, "")
		for _, body := range []string{
			`{"event":"conversation_updated","account":{"id":1}}`,
			`{"event":"message_created","message_type":"incoming","id":1,"account":{"id":1},"conversation":{"id":5,"inbox_id":2}}`,
		} {
			if got := postChatwootWebhook(t, f.app, webhookAuthRequest{body: body}); got != fiber.StatusUnauthorized {
				t.Errorf("status = %d for %s, want 401", got, body)
			}
		}
		if sends := f.send.taken(); len(sends) != 0 {
			t.Errorf("sent %v, want nothing sent", sends)
		}
	})

	t.Run("unsigned opt-in acknowledges events with no effective secret", func(t *testing.T) {
		prev := config.ChatwootWebhookAllowUnsigned
		config.ChatwootWebhookAllowUnsigned = true
		t.Cleanup(func() { config.ChatwootWebhookAllowUnsigned = prev })

		f := newRouteAuthFixture(t, "")
		body := `{"event":"conversation_updated","account":{"id":1}}`
		if got := postChatwootWebhook(t, f.app, webhookAuthRequest{body: body}); got != fiber.StatusOK {
			t.Fatalf("status = %d, want 200 when unsigned webhooks are enabled", got)
		}
		protected := `{"event":"message_created","message_type":"outgoing","id":77,"content":"hi","account":{"id":1},"conversation":{"id":5,"inbox_id":2,"meta":{"sender":{"phone_number":"+628999999999"}}}}`
		if got := postChatwootWebhook(t, f.app, webhookAuthRequest{body: protected}); got != fiber.StatusUnauthorized {
			t.Fatalf("protected device status = %d, want 401 even when the unsigned opt-in is on", got)
		}
		if sends := f.send.taken(); len(sends) != 0 {
			t.Fatalf("sent %v, want nothing sent", sends)
		}
	})
}

// TestChatwootSharedWebhookDeleteEnforcesRoutedDeviceSecret: deleting a
// message in Chatwoot revokes it on WhatsApp, so it passes the same gate.
func TestChatwootSharedWebhookDeleteEnforcesRoutedDeviceSecret(t *testing.T) {
	prev := config.ChatwootMessageDelete
	config.ChatwootMessageDelete = true
	t.Cleanup(func() { config.ChatwootMessageDelete = prev })

	for _, global := range []string{"", "global-secret"} {
		t.Run("global="+global, func(t *testing.T) {
			f := newRouteAuthFixture(t, global)
			f.repo.links = []*domainChatStorage.ChatwootMessageLink{{
				DeviceID: routeAuthSalesJID, WhatsAppMessageID: "WA-OUT-1", WhatsAppChatJID: "628999999999@s.whatsapp.net",
				ChatwootMessageID: 55, Direction: "outgoing", ChatwootConfigID: 1,
			}}
			body := `{"event":"message_updated","message_type":"outgoing","id":55,"content_attributes":{"deleted":true},` +
				`"account":{"id":1},"conversation":{"id":5,"inbox_id":2}}`

			for _, tt := range []struct {
				name, path, signWith string
				want                 int
				wantRevoke           bool
			}{
				{name: "shared, unsigned", want: 401},
				{name: "shared, global secret", signWith: global, want: 401},
				{name: "shared, another device's secret", signWith: routeAuthSupportSecret, want: 401},
				{name: "device endpoint, another device's secret", path: "/chatwoot/webhook/sales", signWith: routeAuthSupportSecret, want: 401},
				{name: "shared, the routed device's secret", signWith: routeAuthSalesSecret, want: 200, wantRevoke: true},
				{name: "device endpoint, device secret", path: "/chatwoot/webhook/sales", signWith: routeAuthSalesSecret, want: 200, wantRevoke: true},
			} {
				t.Run(tt.name, func(t *testing.T) {
					chatwootReplays.reset()
					f.message.revoked = nil
					got := postChatwootWebhook(t, f.app, webhookAuthRequest{path: tt.path, body: body, headers: chatwootSigned(tt.signWith, body)})
					if got != tt.want {
						t.Errorf("status = %d, want %d", got, tt.want)
					}
					if revoked := len(f.message.revoked) == 1 && f.message.revoked[0] == "sales revokes WA-OUT-1"; revoked != tt.wantRevoke {
						t.Errorf("revoked = %v, want revoke=%v", f.message.revoked, tt.wantRevoke)
					}
				})
			}

			// The same Chatwoot message id linked under another configuration
			// (another server's, or legacy config 0) names an unrelated
			// customer's message: a delete through sales must not revoke it.
			for _, foreign := range []int64{0, 2} {
				f.repo.links[0].ChatwootConfigID = foreign
				for _, path := range []string{"", "/chatwoot/webhook/sales"} {
					chatwootReplays.reset()
					f.message.revoked = nil
					got := postChatwootWebhook(t, f.app, webhookAuthRequest{path: path, body: body, headers: chatwootSigned(routeAuthSalesSecret, body)})
					if got != fiber.StatusOK || len(f.message.revoked) != 0 {
						t.Errorf("link under config %d, path %q: status = %d, revoked = %v; want 200 and nothing revoked", foreign, path, got, f.message.revoked)
					}
				}
			}
		})
	}
}

// TestChatwootSignedWebhookReplayIsNotDeliveredTwice pins what a captured
// request can do. A Chatwoot-signed request is delivered once: a second copy
// inside the tolerance window is acknowledged and not acted on. The guard is
// in-process memory, so a restart inside the window forgets it, and the older
// schemes (static secret header, ?secret=, X-Hub-Signature-256) carry no
// timestamp and are not covered at all.
func TestChatwootSignedWebhookReplayIsNotDeliveredTwice(t *testing.T) {
	f := newRouteAuthFixture(t, "")

	for _, path := range []string{"/chatwoot/webhook/sales", "/chatwoot/webhook"} {
		body := agentReplyBody(2, 5)
		captured := webhookAuthRequest{path: path, body: body, headers: chatwootSigned(routeAuthSalesSecret, body)}
		for attempt := 1; attempt <= 3; attempt++ {
			if got := postChatwootWebhook(t, f.app, captured); got != fiber.StatusOK {
				t.Fatalf("%s attempt %d: status = %d, want 200", path, attempt, got)
			}
		}
		if sends := f.send.taken(); len(sends) != 1 {
			t.Fatalf("%s: three copies of one signed request sent %v, want one send", path, sends)
		}

		// A new delivery of the same message (Chatwoot signs with a new
		// timestamp) is a different request and is not held back here.
		later := nowUnix(time.Second)
		redelivery := webhookAuthRequest{path: path, body: body, headers: map[string]string{
			chatwootTimestampHeader: later, chatwootSignatureHeader: chatwootSign(routeAuthSalesSecret, later, body),
		}}
		if got := postChatwootWebhook(t, f.app, redelivery); got != fiber.StatusOK {
			t.Fatalf("%s redelivery: status = %d, want 200", path, got)
		}
		if sends := f.send.taken(); len(sends) != 1 {
			t.Fatalf("%s: a redelivery with a new timestamp sent %v, want one send", path, sends)
		}

		// Not covered: a restart inside the window forgets what was seen.
		chatwootReplays.reset()
		if got := postChatwootWebhook(t, f.app, captured); got != fiber.StatusOK {
			t.Fatalf("%s after restart: status = %d, want 200", path, got)
		}
		if sends := f.send.taken(); len(sends) != 1 {
			t.Fatalf("%s: the captured request after a restart sent %v, want one send (the guard is in memory only)", path, sends)
		}
	}

	// Not covered: the static-secret schemes have nothing that identifies one
	// delivery, so a captured request is delivered every time it is sent.
	body := agentReplyBody(2, 5)
	static := webhookAuthRequest{path: "/chatwoot/webhook/sales", body: body, headers: map[string]string{"X-Chatwoot-Webhook-Secret": routeAuthSalesSecret}}
	for attempt := 1; attempt <= 2; attempt++ {
		if got := postChatwootWebhook(t, f.app, static); got != fiber.StatusOK {
			t.Fatalf("static secret attempt %d: status = %d, want 200", attempt, got)
		}
	}
	if sends := f.send.taken(); len(sends) != 2 {
		t.Fatalf("two copies of a static-secret request sent %v, want two sends", sends)
	}
}

// routeAuthFailingRepo cannot read device configs.
type routeAuthFailingRepo struct {
	*routeAuthRepo
}

func (routeAuthFailingRepo) GetChatwootDeviceConfigByIdentifier(string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	return nil, fmt.Errorf("database is locked")
}

// TestChatwootSharedWebhookFailsClosedOnStorageError: when the routed device's
// config cannot be read it is unknown whether it has its own secret, so
// nothing is sent even with no global secret configured.
func TestChatwootSharedWebhookFailsClosedOnStorageError(t *testing.T) {
	f := newRouteAuthFixture(t, "")
	// A linked conversation routes without the registry, which would itself
	// need the failing lookup.
	f.repo.links = []*domainChatStorage.ChatwootMessageLink{{
		DeviceID: routeAuthSalesJID, WhatsAppChatJID: "628999999999@s.whatsapp.net",
		ChatwootConversationID: 9, ChatwootAccountID: 1, ChatwootConfigID: 1,
	}}
	handler := &ChatwootHandler{SendUsecase: f.send, DeviceManager: whatsapp.NewDeviceManager(nil, nil, nil), ChatStorageRepo: routeAuthFailingRepo{f.repo}}
	app := fiber.New()
	app.Post("/chatwoot/webhook", handler.HandleWebhook)

	if got := postChatwootWebhook(t, app, webhookAuthRequest{body: agentReplyBody(2, 9)}); got != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", got)
	}
	if sends := f.send.taken(); len(sends) != 0 {
		t.Fatalf("sent %v, want nothing sent", sends)
	}
}

// TestChatwootSignedWebhookRedeliveryAfterFailureIsProcessed: a signed request
// answered 503 (here the routed device's config could not be read) was not
// handled, so the identical request sent again must be processed, not
// acknowledged as a repeat. Once it has been accepted, a further copy is one.
func TestChatwootSignedWebhookRedeliveryAfterFailureIsProcessed(t *testing.T) {
	const global = "global-secret"
	f := newRouteAuthFixture(t, global)
	// A linked conversation routes without the registry, which would itself
	// need the failing lookup.
	f.repo.links = []*domainChatStorage.ChatwootMessageLink{{
		DeviceID: routeAuthOpenJID, WhatsAppChatJID: "628999999999@s.whatsapp.net",
		ChatwootConversationID: 9, ChatwootAccountID: 1, ChatwootConfigID: 3,
	}}
	body := agentReplyBody(4, 9)
	request := webhookAuthRequest{body: body, headers: chatwootSigned(global, body)}

	f.repo.failLookups(fmt.Errorf("database is locked"))
	if got := postChatwootWebhook(t, f.app, request); got != fiber.StatusServiceUnavailable {
		t.Fatalf("while storage fails: status = %d, want 503", got)
	}
	if sends := f.send.taken(); len(sends) != 0 {
		t.Fatalf("while storage fails: sent %v, want nothing sent", sends)
	}

	f.repo.failLookups(nil)
	if got := postChatwootWebhook(t, f.app, request); got != fiber.StatusOK {
		t.Fatalf("redelivery: status = %d, want 200", got)
	}
	if sends := f.send.taken(); len(sends) != 1 {
		t.Fatalf("redelivery of a request answered 503 sent %v, want one send", sends)
	}

	if got := postChatwootWebhook(t, f.app, request); got != fiber.StatusOK {
		t.Fatalf("repeat: status = %d, want 200", got)
	}
	if sends := f.send.taken(); len(sends) != 0 {
		t.Fatalf("a copy of an accepted request sent %v, want nothing sent", sends)
	}
}
