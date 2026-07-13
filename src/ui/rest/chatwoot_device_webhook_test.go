package rest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/gofiber/fiber/v2"
)

// deviceWebhookTestRepo satisfies the few repo methods the ClientRegistry and
// route resolver call; the rest of the interface is the embedded (nil) value.
type deviceWebhookTestRepo struct {
	domainChatStorage.IChatStorageRepository
	cfg   *domainChatStorage.ChatwootDeviceConfig
	count int
}

func (r *deviceWebhookTestRepo) GetChatwootDeviceConfigByIdentifier(identifier string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	if r.cfg != nil && (identifier == r.cfg.DeviceID || identifier == r.cfg.DeviceJID) {
		return r.cfg, nil
	}
	return nil, nil
}

func (r *deviceWebhookTestRepo) GetChatwootDeviceConfigByInbox(accountID, inboxID int) (*domainChatStorage.ChatwootDeviceConfig, error) {
	return nil, nil
}

func (r *deviceWebhookTestRepo) GetLatestChatwootMessageLinkByConversation(conversationID, accountID int, allowLegacyZero bool, configID int64) (*domainChatStorage.ChatwootMessageLink, error) {
	return nil, nil
}

func (r *deviceWebhookTestRepo) CountChatwootDeviceConfigs() (int, error) {
	return r.count, nil
}

// TestHandleDeviceWebhookValidatesAccountInbox proves the per-device endpoint is
// route-by-config: a payload whose account/inbox does not match the device's
// configured account/inbox is rejected (401), while a matching payload passes
// the gate (here a non-action event, so it returns 200 without sending).
func TestHandleDeviceWebhookValidatesAccountInbox(t *testing.T) {
	repo := &deviceWebhookTestRepo{
		cfg:   &domainChatStorage.ChatwootDeviceConfig{ID: 1, DeviceID: "d", ChatwootURL: "https://chat.example.com", AccountID: 1, InboxID: 2, APIToken: "t", Enabled: true},
		count: 1,
	}
	chatwoot.InitClientRegistry(repo)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	handler := &ChatwootHandler{ChatStorageRepo: repo}
	app := fiber.New()
	app.Post("/chatwoot/webhook/:device_id", handler.HandleDeviceWebhook)

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/chatwoot/webhook/d", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		return resp.StatusCode
	}

	// Mismatched account -> rejected.
	if got := post(`{"event":"message_created","message_type":"outgoing","account":{"id":999},"conversation":{"id":5,"inbox_id":2}}`); got != fiber.StatusUnauthorized {
		t.Fatalf("account mismatch status = %d, want 401", got)
	}
	// Mismatched inbox -> rejected.
	if got := post(`{"event":"message_created","message_type":"outgoing","account":{"id":1},"conversation":{"id":5,"inbox_id":77}}`); got != fiber.StatusUnauthorized {
		t.Fatalf("inbox mismatch status = %d, want 401", got)
	}
	// Matching account+inbox passes the gate (incoming message -> 200, no send).
	if got := post(`{"event":"message_created","message_type":"incoming","account":{"id":1},"conversation":{"id":5,"inbox_id":2}}`); got != fiber.StatusOK {
		t.Fatalf("matching status = %d, want 200", got)
	}
	// Non-message events lack the fields the gate checks; they must be
	// acknowledged, not 401-ed, even when account/inbox don't match.
	if got := post(`{"event":"conversation_updated","account":{"id":999}}`); got != fiber.StatusOK {
		t.Fatalf("non-message event status = %d, want 200", got)
	}
}

// TestProcessChatwootWebhookDropsUnroutablePayload proves the fail-fast is
// enforced at delivery: in per-device mode an unmapped conversation must be
// acknowledged WITHOUT resolving a device — an empty DeviceID would otherwise
// fall through to the default device and send from the wrong WhatsApp account.
func TestProcessChatwootWebhookDropsUnroutablePayload(t *testing.T) {
	chatwoot.InitClientRegistry(nil)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	// Non-empty config table, no link/attr/inbox mapping for the payload.
	handler := &ChatwootHandler{ChatStorageRepo: &deviceWebhookTestRepo{count: 2}}
	route := handler.resolveChatwootWebhookRoute(chatwoot.WebhookPayload{
		Account:      chatwoot.Account{ID: 1},
		Conversation: chatwoot.ConversationWebhook{ID: 5, InboxID: 9},
	}, nil)
	if !route.Unroutable {
		t.Fatalf("unmapped payload in per-device mode must be marked unroutable, got %+v", route)
	}

	// End-to-end: the webhook handler must return 200 without touching the
	// device manager (nil here — a delivery attempt would resolve the default
	// device and panic on the nil usecases before responding).
	app := fiber.New()
	app.Post("/chatwoot/webhook", handler.HandleWebhook)
	body := `{"event":"message_created","message_type":"outgoing","account":{"id":1},"conversation":{"id":5,"inbox_id":9,"meta":{"sender":{"phone_number":"+628999999999"}}},"content":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/chatwoot/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("unroutable payload status = %d, want 200", resp.StatusCode)
	}

	// Legacy mode stays deliverable: the same payload resolves the env device.
	prev := config.ChatwootDeviceID
	config.ChatwootDeviceID = "env-default-device"
	t.Cleanup(func() { config.ChatwootDeviceID = prev })
	legacy := &ChatwootHandler{ChatStorageRepo: &deviceWebhookTestRepo{count: 0}}
	route = legacy.resolveChatwootWebhookRoute(chatwoot.WebhookPayload{
		Account:      chatwoot.Account{ID: 1},
		Conversation: chatwoot.ConversationWebhook{ID: 5, InboxID: 9},
	}, nil)
	if route.Unroutable {
		t.Fatalf("legacy mode must not mark payloads unroutable, got %+v", route)
	}
}

// TestResolveChatwootWebhookRouteFailsFastWhenConfigsExist proves that once any
// per-device config exists, an unmapped conversation does NOT fall back to the
// env ChatwootDeviceID (which would mis-deliver across inboxes).
func TestResolveChatwootWebhookRouteFailsFastWhenConfigsExist(t *testing.T) {
	prev := config.ChatwootDeviceID
	config.ChatwootDeviceID = "env-default-device"
	t.Cleanup(func() { config.ChatwootDeviceID = prev })
	chatwoot.InitClientRegistry(nil)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	// Repo: no conversation link, but the config table is non-empty.
	handler := &ChatwootHandler{ChatStorageRepo: &deviceWebhookTestRepo{count: 2}}

	route := handler.resolveChatwootWebhookRoute(chatwoot.WebhookPayload{
		Account:      chatwoot.Account{ID: 1},
		Conversation: chatwoot.ConversationWebhook{ID: 5, InboxID: 9},
	}, nil)
	if route.DeviceID != "" {
		t.Fatalf("expected fail-fast (empty device), got %q", route.DeviceID)
	}

	// Sanity: with an empty config table, env fallback applies.
	handlerLegacy := &ChatwootHandler{ChatStorageRepo: &deviceWebhookTestRepo{count: 0}}
	route = handlerLegacy.resolveChatwootWebhookRoute(chatwoot.WebhookPayload{
		Account:      chatwoot.Account{ID: 1},
		Conversation: chatwoot.ConversationWebhook{ID: 5, InboxID: 9},
	}, nil)
	if route.DeviceID != "env-default-device" {
		t.Fatalf("legacy mode should use env device, got %q", route.DeviceID)
	}
}
