package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/fiber/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// deleteRepo serves the links of Chatwoot message 90: WA-1 and WA-2 went
// out from account 3, WA-IN was received and WA-X belongs to account 4.
// Message 91 has WA-3 from account 3 and WA-0, a legacy account-0 link.
type deleteRepo struct {
	chatwootRouteTestRepo
	calls int
}

func (r *deleteRepo) ListChatwootMessageLinksByChatwootID(deviceID string, chatwootMessageID int) ([]*domainChatStorage.ChatwootMessageLink, error) {
	r.calls++
	links := []*domainChatStorage.ChatwootMessageLink{}
	ids := map[int][]string{90: {"WA-1", "WA-2", "WA-IN", "WA-X"}, 91: {"WA-0", "WA-3"}}[chatwootMessageID]
	for _, id := range ids {
		link := &domainChatStorage.ChatwootMessageLink{DeviceID: deviceID, WhatsAppMessageID: id,
			WhatsAppChatJID: "5500900000001@s.whatsapp.net", ChatwootMessageID: chatwootMessageID, ChatwootAccountID: 3, Direction: "outgoing"}
		switch id {
		case "WA-IN":
			link.Direction = "incoming"
		case "WA-X":
			link.ChatwootAccountID = 4
		case "WA-0":
			link.ChatwootAccountID = 0
		}
		links = append(links, link)
	}
	return links, nil
}

// deleteUsecase records each revoke and fails the ones in fail.
type deleteUsecase struct {
	domainMessage.IMessageUsecase
	ids  []string
	fail map[string]error
}

func (u *deleteUsecase) RevokeMessage(_ context.Context, r domainMessage.RevokeRequest) (domainMessage.GenericResponse, error) {
	u.ids = append(u.ids, r.MessageID)
	return domainMessage.GenericResponse{}, u.fail[r.MessageID]
}

type deleteEnv struct {
	app     *fiber.App
	usecase *deleteUsecase
	repo    *deleteRepo
	notes   []string
}

func newDeleteEnv(t *testing.T) *deleteEnv {
	t.Helper()
	env := &deleteEnv{usecase: &deleteUsecase{fail: map[string]error{}}, repo: &deleteRepo{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		env.notes = append(env.notes, fmt.Sprint(body["content"]))
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(server.Close)

	prevURL, prevToken := config.ChatwootURL, config.ChatwootAPIToken
	prevAccount, prevInbox, prevDelete := config.ChatwootAccountID, config.ChatwootInboxID, config.ChatwootMessageDelete
	t.Cleanup(func() {
		config.ChatwootURL, config.ChatwootAPIToken = prevURL, prevToken
		config.ChatwootAccountID, config.ChatwootInboxID, config.ChatwootMessageDelete = prevAccount, prevInbox, prevDelete
		chatwoot.InitClientRegistry(nil)
	})
	config.ChatwootURL, config.ChatwootAPIToken = server.URL, "tok"
	config.ChatwootAccountID, config.ChatwootInboxID, config.ChatwootMessageDelete = 3, 5, true
	chatwoot.InitClientRegistry(nil)

	jid := types.NewJID("device-a", types.DefaultUserServer)
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	dm.AddDevice(whatsapp.NewDeviceInstance("slot-a", &whatsmeow.Client{Store: &store.Device{ID: &jid}}, nil))
	handler := &ChatwootHandler{MessageUsecase: env.usecase, DeviceManager: dm, ChatStorageRepo: env.repo}
	env.app = fiber.New()
	env.app.Post("/chatwoot/webhook", handler.HandleWebhook)
	return env
}

// deleted delivers Chatwoot's message_updated for a deleted outgoing message.
func (e *deleteEnv) deleted(t *testing.T, id int) {
	t.Helper()
	body := fmt.Sprintf(`{"event":"message_updated","message_type":"outgoing","id":%d,"content_attributes":{"deleted":true},`+
		`"account":{"id":3},"conversation":{"id":7,"inbox_id":5,"meta":{"sender":{"id":11,"phone_number":"+5500900000001"}}}}`, id)
	if resp, raw := doJSON(t, e.app, http.MethodPost, "/chatwoot/webhook", body); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("webhook status = %d (%s), want 200", resp.StatusCode, raw)
	}
}

func deleteEqual(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", what, got, want)
	}
}

func TestChatwootDeleteRevokesEveryOutgoingLink(t *testing.T) {
	env := newDeleteEnv(t)
	env.deleted(t, 90)
	deleteEqual(t, "revokes", env.usecase.ids, []string{"WA-1", "WA-2"})
	deleteEqual(t, "notes", len(env.notes), 0)
}

// A legacy account-0 link is revoked only while no per-device config exists,
// as in the conversation lookup.
func TestChatwootDeleteRevokesLegacyLinksOnlyInLegacyMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		configs int
		revokes []string
	}{
		{"legacy mode", 0, []string{"WA-0", "WA-3"}},
		{"per-device mode", 1, []string{"WA-3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newDeleteEnv(t)
			env.repo.count = tc.configs
			env.repo.link = &domainChatStorage.ChatwootMessageLink{DeviceID: "slot-a", WhatsAppChatJID: "5500900000001@s.whatsapp.net",
				ChatwootConversationID: 7, ChatwootAccountID: 3}
			env.deleted(t, 91)
			deleteEqual(t, "revokes", env.usecase.ids, tc.revokes)
		})
	}
}
