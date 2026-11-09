package rest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/gofiber/fiber/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

const (
	bindingOldJID = "628111111111@s.whatsapp.net"
	bindingNewJID = "628222222222@s.whatsapp.net"
	bindingPeer   = "628999999999@s.whatsapp.net"
)

type webhookBindingFixture struct {
	app          *fiber.App
	db           *sql.DB
	repo         domainChatStorage.IChatStorageRepository
	manager      *whatsapp.DeviceManager
	old, current *domainChatStorage.ChatwootDeviceConfig
	send         *bindingSend
	message      *routeAuthMessage
	notes        atomic.Int64
}

type bindingSend struct{ routeAuthSend }

func (s *bindingSend) SendText(ctx context.Context, req domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	if instance, ok := whatsapp.DeviceFromContext(ctx); !ok || instance == nil || instance.GetClient() == nil {
		return domainSend.GenericResponse{}, errors.New("device not logged in")
	}
	return s.routeAuthSend.SendText(ctx, req)
}

func newWebhookBindingFixture(t *testing.T) *webhookBindingFixture {
	t.Helper()
	withChatwootWebhookSecret(t, "global-secret")
	oldDelete, oldRevoke, oldDevice, oldRead := config.ChatwootMessageDelete, config.ChatwootRevokeOnDelete, config.ChatwootDeviceID, config.ChatwootMessageRead
	oldHosts := config.ChatwootAllowedHosts
	config.ChatwootMessageDelete, config.ChatwootRevokeOnDelete, config.ChatwootMessageRead = false, true, false
	config.ChatwootDeviceID, config.ChatwootAllowedHosts = "", []string{"127.0.0.1"}
	chatwootReplays.reset()
	t.Cleanup(func() {
		config.ChatwootMessageDelete, config.ChatwootRevokeOnDelete, config.ChatwootDeviceID, config.ChatwootMessageRead = oldDelete, oldRevoke, oldDevice, oldRead
		config.ChatwootAllowedHosts = oldHosts
		chatwoot.InitClientRegistry(nil)
		chatwootReplays.reset()
	})
	f := &webhookBindingFixture{send: &bindingSend{}, message: &routeAuthMessage{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.notes.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":200}`))
	}))
	t.Cleanup(server.Close)
	var err error
	f.db, err = sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "chatwoot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Close() })
	f.repo = chatstorage.NewStorageRepository(f.db)
	if err = f.repo.InitializeSchema(); err != nil {
		t.Fatal(err)
	}
	f.old = &domainChatStorage.ChatwootDeviceConfig{DeviceID: "old", DeviceJID: bindingOldJID, ChatwootURL: server.URL + "/old", AccountID: 3, InboxID: 5, APIToken: "local", Enabled: true, WebhookSecret: "old-secret"}
	f.save(t, f.old)
	if _, err = f.repo.UpdateChatwootDeviceConfigJID("old", bindingNewJID); err != nil {
		t.Fatal(err)
	}
	f.old.DeviceJID = bindingNewJID
	f.current = &domainChatStorage.ChatwootDeviceConfig{DeviceID: "current", DeviceJID: bindingOldJID, ChatwootURL: server.URL + "/current", AccountID: 3, InboxID: 5, APIToken: "local", Enabled: true, WebhookSecret: "current-secret"}
	f.save(t, f.current)
	for i, seed := range []struct {
		id, device, direction string
		cfg                   int64
		conversation, account int
	}{
		{"historical-1", bindingOldJID, "outgoing", f.old.ID, 7, 3},
		{"historical-2", bindingOldJID, "outgoing", f.old.ID, 7, 3},
		{"current-1", bindingOldJID, "outgoing", f.current.ID, 7, 3},
		{"current-2", bindingOldJID, "outgoing", f.current.ID, 7, 3},
		{"incoming", bindingOldJID, "incoming", f.current.ID, 7, 3},
		{"old-current", bindingNewJID, "outgoing", f.old.ID, 8, 3},
		{"legacy-account", bindingOldJID, "outgoing", 0, 7, 3},
		{"legacy-zero", bindingOldJID, "outgoing", 0, 7, 0},
		{"legacy-other-account", bindingOldJID, "outgoing", 0, 7, 4},
	} {
		if err = f.repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
			DeviceID: seed.device, WhatsAppMessageID: seed.id, WhatsAppChatJID: bindingPeer,
			ChatwootMessageID: 90, ChatwootConversationID: seed.conversation, ChatwootInboxID: 5,
			ChatwootAccountID: seed.account, ChatwootConfigID: seed.cfg,
			Direction: seed.direction, CreatedAt: time.Unix(int64(100+i), 0),
		}); err != nil {
			t.Fatal(err)
		}
	}
	chatwoot.InitClientRegistry(f.repo)
	f.manager = whatsapp.NewDeviceManager(nil, nil, nil)
	for alias, jid := range map[string]string{"old": bindingNewJID, "current": bindingOldJID} {
		id, err := types.ParseJID(jid)
		if err != nil {
			t.Fatal(err)
		}
		f.manager.AddDevice(whatsapp.NewDeviceInstance(alias, &whatsmeow.Client{Store: &store.Device{ID: &id}}, f.repo))
	}
	handler := &ChatwootHandler{SendUsecase: f.send, MessageUsecase: f.message, DeviceManager: f.manager, ChatStorageRepo: f.repo}
	f.app = fiber.New()
	f.app.Use("/chatwoot/webhook", ChatwootWebhookReplayGuard())
	f.app.Post("/chatwoot/webhook", handler.HandleWebhook)
	f.app.Post("/chatwoot/webhook/:device_id", handler.HandleDeviceWebhook)
	return f
}

func (f *webhookBindingFixture) save(t *testing.T, cfg *domainChatStorage.ChatwootDeviceConfig) {
	t.Helper()
	if err := f.repo.SaveChatwootDeviceConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func (f *webhookBindingFixture) linkCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM chatwoot_message_links").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func (f *webhookBindingFixture) post(t *testing.T, path, event, secret, override string, conversation int) int {
	t.Helper()
	body := fmt.Sprintf(`{"event":%q,"message_type":"outgoing","content":"hello","id":90,"content_attributes":{"deleted":true},"account":{"id":3},"conversation":{"id":%d,"inbox_id":5,"meta":{"sender":{"phone_number":"+628999999999","custom_attributes":{"gowa_device_id":%q}}}}}`, event, conversation, override)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for name, value := range chatwootSigned(secret, body) {
		req.Header.Set(name, value)
	}
	response, err := f.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func TestChatwootWebhookBindsConfigToLiveDevice(t *testing.T) {
	for _, warm := range []bool{false, true} {
		for _, tc := range []struct {
			name, path, secret, override string
			disabled, unstamped, stale   bool
			movedInbox                   bool
			conversation                 int
			wantDevice                   string
			wantRevokes                  []string
			status                       int
		}{
			{name: "disabled owner's secret cannot authorize historical config", secret: "current-secret", disabled: true, override: "current", conversation: 7, status: 401},
			{name: "historical config's secret cannot drive the rebound device", secret: "old-secret", disabled: true, override: "current", conversation: 7, status: 401},
			{name: "live slot wins over stale config JID", secret: "old-secret", disabled: true, stale: true, conversation: 7, status: 401},
			{name: "colliding enabled inbox needs an explicit device", secret: "current-secret", conversation: 7, status: 401},
			{name: "explicit device accepts its own secret", secret: "current-secret", override: "current", conversation: 7, wantDevice: "current", wantRevokes: []string{"current revokes current-1", "current revokes current-2"}, status: 200},
			{name: "explicit device rejects other server's secret", secret: "old-secret", override: "current", conversation: 7, status: 401},
			{name: "per-device URL accepts its own secret", path: "/current", secret: "current-secret", conversation: 7, wantDevice: "current", wantRevokes: []string{"current revokes current-1", "current revokes current-2"}, status: 200},
			{name: "per-device URL rejects other server's secret", path: "/current", secret: "old-secret", conversation: 7, status: 401},
			{name: "disabled per-device config cannot act", path: "/current", secret: "current-secret", disabled: true, conversation: 7, status: 200},
			{name: "cached account and inbox cannot authorize changed config", path: "/current", secret: "current-secret", movedInbox: true, conversation: 7, status: 401},
			{name: "current conversation remains bound to original slot", secret: "old-secret", disabled: true, conversation: 8, wantDevice: "old", wantRevokes: []string{"old revokes old-current"}, status: 200},
			{name: "paired slot authenticates before config JID is stamped", path: "/current", secret: "current-secret", unstamped: true, conversation: 7, wantDevice: "current", wantRevokes: []string{"current revokes current-1", "current revokes current-2"}, status: 200},
		} {
			for _, event := range []string{"message_updated", "message_created"} {
				t.Run(fmt.Sprintf("warm=%v/%s/%s", warm, tc.name, event), func(t *testing.T) {
					f := newWebhookBindingFixture(t)
					if warm {
						for _, id := range []string{"old", "current", bindingOldJID} {
							if _, err := chatwoot.GetClientRegistry().Resolve(id); err != nil {
								t.Fatal(err)
							}
						}
					}
					if tc.disabled {
						f.current.Enabled = false
						f.save(t, f.current)
					}
					if tc.unstamped {
						f.current.DeviceJID = ""
						f.save(t, f.current)
					}
					if tc.stale {
						f.current.DeviceJID = ""
						f.save(t, f.current)
						f.old.DeviceJID = bindingOldJID
						f.save(t, f.old)
					}
					if tc.movedInbox {
						f.current.AccountID, f.current.InboxID = 4, 6
						f.save(t, f.current)
					}
					before := f.linkCount(t)
					status := f.post(t, "/chatwoot/webhook"+tc.path, event, tc.secret, tc.override, tc.conversation)
					var wantSends, wantRevokes []string
					if event == "message_created" && tc.wantDevice != "" {
						wantSends = []string{tc.wantDevice + " -> 628999999999"}
						before++
					} else if event == "message_updated" {
						wantRevokes = tc.wantRevokes
					}
					if !reflect.DeepEqual(f.message.revoked, wantRevokes) {
						t.Errorf("revokes = %v, want %v", f.message.revoked, wantRevokes)
					}
					if sends := f.send.taken(); !reflect.DeepEqual(sends, wantSends) {
						t.Errorf("sends = %v, want %v", sends, wantSends)
					}
					if after := f.linkCount(t); after != before {
						t.Errorf("stored links = %d, want %d", after, before)
					}
					if notes := f.notes.Load(); notes != 0 {
						t.Errorf("Chatwoot requests = %d, want 0", notes)
					}
					if status != tc.status {
						t.Errorf("status = %d, want %d", status, tc.status)
					}
				})
			}
		}
	}
}

func TestChatwootWebhookSignedLegacyDeleteKeepsAccountScope(t *testing.T) {
	f := newWebhookBindingFixture(t)
	for _, id := range []string{"old", "current"} {
		if err := f.repo.DeleteChatwootDeviceConfig(id); err != nil {
			t.Fatal(err)
		}
	}
	f.manager.RemoveDevice("old")
	status := f.post(t, "/chatwoot/webhook", "message_updated", "global-secret", "", 9)
	want := []string{"current revokes legacy-account", "current revokes legacy-zero"}
	if !reflect.DeepEqual(f.message.revoked, want) || status != 200 {
		t.Fatalf("status = %d, revokes = %v, want 200 and %v", status, f.message.revoked, want)
	}
}

func TestChatwootWebhookSignedUnpairedSlotReportsFailure(t *testing.T) {
	f := newWebhookBindingFixture(t)
	slot, _ := f.manager.GetDevice("current")
	slot.ResetClient()
	f.current.DeviceJID = ""
	f.save(t, f.current)
	before := f.linkCount(t)
	status := f.post(t, "/chatwoot/webhook/current", "message_created", "current-secret", "", 7)
	if status != 200 || f.notes.Load() != 1 || len(f.send.taken()) != 0 || len(f.message.revoked) != 0 || f.linkCount(t) != before {
		t.Fatalf("unpaired slot: status = %d, failure notes = %d, links = %d", status, f.notes.Load(), f.linkCount(t))
	}
}
