package rest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/fiber/v3"
)

// recordingTextSender captures the text an agent reply is delivered with; the
// rest of the send usecase is the embedded (nil) interface.
type recordingTextSender struct {
	domainSend.ISendUsecase
	sent []string
}

func (s *recordingTextSender) SendText(_ context.Context, req domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	s.sent = append(s.sent, req.Message)
	return domainSend.GenericResponse{}, nil
}

// signTestRepo adds a stored conversation link to deviceWebhookTestRepo so a
// reply can be routed on the shared webhook endpoint.
type signTestRepo struct {
	deviceWebhookTestRepo
	link *domainChatStorage.ChatwootMessageLink
}

func (r *signTestRepo) GetLatestChatwootMessageLinkByConversation(int, int, bool, int64) (*domainChatStorage.ChatwootMessageLink, error) {
	return r.link, nil
}

// TestAgentReplySignatureFollowsDeviceConfig drives an agent reply through the
// webhook handlers and checks the text handed to the send usecase: the sending
// device's sign_msg wins over CHATWOOT_SIGN_MSG, an unset sign_msg inherits
// it. A disabled config sends nothing at all, signed or not: the route gate
// refuses delivery before the text is composed, also for a reply that still
// routes through a stored conversation link.
func TestAgentReplySignatureFollowsDeviceConfig(t *testing.T) {
	prevUnsigned := config.ChatwootWebhookAllowUnsigned
	config.ChatwootWebhookAllowUnsigned = true
	t.Cleanup(func() { config.ChatwootWebhookAllowUnsigned = prevUnsigned })
	off := false
	const body = `{"id":77,"event":"message_created","message_type":"outgoing","content":"Hello",` +
		`"account":{"id":1},"sender":{"id":3,"name":"Jane Smith"},` +
		`"conversation":{"id":5,"inbox_id":2,"messages":[{"id":77,"sender":{"id":3,"name":"Jane Smith","available_name":"Jane"}}],"meta":{"sender":{"phone_number":"+628111111111"}}}}`

	tests := []struct {
		name    string
		signMsg *bool
		enabled bool
		path    string
		want    string // "" = nothing may be sent
		status  int    // 0 = 200
	}{
		{name: "device off beats global on", signMsg: &off, enabled: true, path: "/chatwoot/webhook/dev", want: "Hello"},
		{name: "unset inherits global on", signMsg: nil, enabled: true, path: "/chatwoot/webhook/dev", want: "*Jane*\n\nHello"},
		{name: "disabled config sends nothing on the device endpoint", signMsg: &off, enabled: false, path: "/chatwoot/webhook/dev"},
		{name: "disabled config sends nothing through a stored link", signMsg: nil, enabled: false, path: "/chatwoot/webhook", status: fiber.StatusUnauthorized}, // refused at authentication
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSignConfig(t, true, "\n\n")

			repo := &signTestRepo{
				deviceWebhookTestRepo: deviceWebhookTestRepo{
					cfg: &domainChatStorage.ChatwootDeviceConfig{
						ID: 1, DeviceID: "dev", ChatwootURL: "https://chat.example.com",
						AccountID: 1, InboxID: 2, APIToken: "t", Enabled: tt.enabled, SignMsg: tt.signMsg, WebhookSecret: "channel-secret",
					},
					count: 1,
				},
				link: &domainChatStorage.ChatwootMessageLink{
					DeviceID: "dev", WhatsAppChatJID: "628111111111@s.whatsapp.net",
					ChatwootConversationID: 5, ChatwootConfigID: 1, ChatwootAccountID: 1,
				},
			}
			chatwoot.InitClientRegistry(repo)
			t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

			dm := whatsapp.NewDeviceManager(nil, nil, nil)
			dm.AddDevice(whatsapp.NewDeviceInstance("dev", nil, nil))
			sender := &recordingTextSender{}
			handler := &ChatwootHandler{DeviceManager: dm, ChatStorageRepo: repo, SendUsecase: sender}

			app := fiber.New()
			app.Post("/chatwoot/webhook", handler.HandleWebhook)
			app.Post("/chatwoot/webhook/:device_id", handler.HandleDeviceWebhook)

			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Chatwoot-Webhook-Secret", "channel-secret")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			wantStatus := tt.status
			if wantStatus == 0 {
				wantStatus = fiber.StatusOK
			}
			if resp.StatusCode != wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, wantStatus)
			}
			if tt.want == "" {
				if len(sender.sent) != 0 {
					t.Fatalf("sent text = %q, want nothing sent for a disabled config", sender.sent)
				}
				return
			}
			if len(sender.sent) != 1 || sender.sent[0] != tt.want {
				t.Fatalf("sent text = %q, want [%q]", sender.sent, tt.want)
			}
		})
	}
}

func TestAgentReplySignatureUsesPublicDisplayName(t *testing.T) {
	withSignConfig(t, true, "\n\n")
	for _, tt := range []struct {
		name, sender, messages, want string
	}{
		{"webhook nested display name", `{"id":3,"name":"Jane Smith"}`, `[{"id":77,"sender":{"id":3,"available_name":"Jane"}}]`, "Jane"},
		{"recovered API reply", `{"id":3,"name":"Jane Smith","available_name":"Jane"}`, `[]`, "Jane"},
		{"explicit display name", `{"name":"Jane Smith","display_name":"Jane"}`, `[]`, "Jane"},
		{"public name wins", `{"name":"Jane Smith","available_name":"Jane","display_name":"Other"}`, `[]`, "Jane"},
		{"matching message rather than first", `{"id":3,"name":"Jane Smith"}`, `[{"id":76,"sender":{"id":4,"available_name":"Other"}},{"id":77,"sender":{"id":3,"available_name":"Jane"}}]`, "Jane"},
		{"different message ignored", `{"id":3,"name":"Jane Smith"}`, `[{"id":76,"sender":{"id":3,"available_name":"Other"}}]`, "Jane Smith"},
		{"different sender ignored", `{"id":3,"name":"Jane Smith"}`, `[{"id":77,"sender":{"id":4,"available_name":"Other"}}]`, "Jane Smith"},
		{"blank public name falls back", `{"name":"Jane Smith","available_name":"  ","display_name":" "}`, `[]`, "Jane Smith"},
		{"public name trimmed", `{"name":"Jane Smith","available_name":"  Jane  "}`, `[]`, "Jane"},
		{"no name stays unsigned", `{}`, `[]`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var payload chatwoot.WebhookPayload
			err := json.Unmarshal([]byte(`{"id":77,"content":"Hello","sender":`+tt.sender+`,"conversation":{"messages":`+tt.messages+`}}`), &payload)
			if err != nil {
				t.Fatal(err)
			}
			want := "Hello"
			if tt.want != "" {
				want = "*" + tt.want + "*\n\nHello"
			}
			if got := composeOutgoingText(payload, nil); got != want {
				t.Fatalf("outgoing text = %q, want %q", got, want)
			}
		})
	}
}

// failingSignConfigRepo is signTestRepo with a per-device config lookup that
// always fails, as during a storage outage.
type failingSignConfigRepo struct {
	signTestRepo
	lookups int
}

func (r *failingSignConfigRepo) GetChatwootDeviceConfigByIdentifier(string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	r.lookups++
	return nil, errors.New("database is locked")
}

func (r *failingSignConfigRepo) ListChatwootDeviceConfigs() ([]*domainChatStorage.ChatwootDeviceConfig, error) {
	r.lookups++
	return nil, errors.New("database is locked")
}

// Authentication precedes signature composition: an unreadable device config
// must fail closed without sending a reply. With no global secret the shared
// endpoint already refuses the request before reading it.
func TestAgentReplyConfigFailureRefusesDelivery(t *testing.T) {
	withSignConfig(t, true, "\n\n")

	off := false
	repo := &failingSignConfigRepo{signTestRepo: signTestRepo{
		deviceWebhookTestRepo: deviceWebhookTestRepo{
			// The stored row says "do not sign", but it cannot be read.
			cfg: &domainChatStorage.ChatwootDeviceConfig{
				ID: 1, DeviceID: "dev", ChatwootURL: "https://chat.example.com",
				AccountID: 1, InboxID: 2, APIToken: "t", Enabled: true, SignMsg: &off,
			},
			count: 1,
		},
		link: &domainChatStorage.ChatwootMessageLink{
			DeviceID: "dev", WhatsAppChatJID: "628111111111@s.whatsapp.net",
			ChatwootConversationID: 5, ChatwootConfigID: 1, ChatwootAccountID: 1,
		},
	}}
	chatwoot.InitClientRegistry(repo)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	dm.AddDevice(whatsapp.NewDeviceInstance("dev", nil, nil))
	sender := &recordingTextSender{}
	handler := &ChatwootHandler{DeviceManager: dm, ChatStorageRepo: repo, SendUsecase: sender}

	app := fiber.New()
	app.Post("/chatwoot/webhook", handler.HandleWebhook)

	const body = `{"id":78,"event":"message_created","message_type":"outgoing","content":"Hello",` +
		`"account":{"id":1},"sender":{"name":"Jane"},` +
		`"conversation":{"id":5,"inbox_id":2,"meta":{"sender":{"phone_number":"+628111111111"}}}}`
	req := httptest.NewRequest(http.MethodPost, "/chatwoot/webhook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized && resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want the request refused", resp.StatusCode)
	}
	if len(sender.sent) != 0 {
		t.Fatalf("sent text = %q, want no delivery after auth config failure", sender.sent)
	}
}

// A later settings failure must still not turn on global signing. The handler
// regression above separately verifies the earlier authentication failure gate.
func TestSignatureCompositionDoesNotBorrowGlobalOnLookupFailure(t *testing.T) {
	withSignConfig(t, true, "\n\n")
	repo := &failingSignConfigRepo{}
	chatwoot.InitClientRegistry(repo)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })
	handler := &ChatwootHandler{ChatStorageRepo: repo}
	payload := chatwoot.WebhookPayload{Content: "Hello", Sender: chatwoot.Contact{Name: "Jane"}}
	for name, route := range map[string]chatwootWebhookRoute{
		"route config id": {DeviceID: "dev", ConfigID: 1},
		"device registry": {DeviceID: "dev"},
	} {
		if got := composeOutgoingText(payload, handler.signConfigForDevice(route, "dev")); got != "Hello" {
			t.Fatalf("%s: composed text = %q, want unsigned text", name, got)
		}
	}
}
