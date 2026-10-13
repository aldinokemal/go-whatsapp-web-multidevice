package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
// it, and a disabled config keeps its setting for replies that still route
// through a stored conversation link.
func TestAgentReplySignatureFollowsDeviceConfig(t *testing.T) {
	off := false
	const body = `{"id":77,"event":"message_created","message_type":"outgoing","content":"Hello",` +
		`"account":{"id":1},"sender":{"name":"Jane"},` +
		`"conversation":{"id":5,"inbox_id":2,"meta":{"sender":{"phone_number":"+628111111111"}}}}`

	tests := []struct {
		name    string
		signMsg *bool
		enabled bool
		path    string
		want    string
	}{
		{name: "device off beats global on", signMsg: &off, enabled: true, path: "/chatwoot/webhook/dev", want: "Hello"},
		{name: "unset inherits global on", signMsg: nil, enabled: true, path: "/chatwoot/webhook/dev", want: "*Jane*\n\nHello"},
		{name: "disabled config still applies its setting", signMsg: &off, enabled: false, path: "/chatwoot/webhook", want: "Hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withSignConfig(t, true, "\n\n")

			repo := &signTestRepo{
				deviceWebhookTestRepo: deviceWebhookTestRepo{
					cfg: &domainChatStorage.ChatwootDeviceConfig{
						ID: 1, DeviceID: "dev", ChatwootURL: "https://chat.example.com",
						AccountID: 1, InboxID: 2, APIToken: "t", Enabled: tt.enabled, SignMsg: tt.signMsg,
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
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}
			if len(sender.sent) != 1 || sender.sent[0] != tt.want {
				t.Fatalf("sent text = %q, want [%q]", sender.sent, tt.want)
			}
		})
	}
}

// failingConfigRepo is signTestRepo with a per-device config lookup that
// always fails, as during a storage outage.
type failingConfigRepo struct {
	signTestRepo
	lookups int
}

func (r *failingConfigRepo) GetChatwootDeviceConfigByIdentifier(string) (*domainChatStorage.ChatwootDeviceConfig, error) {
	r.lookups++
	return nil, errors.New("database is locked")
}

// TestAgentReplyIsUnsignedWhenSignatureLookupFails covers the fail-closed
// rule: with CHATWOOT_SIGN_MSG on and the device's sign_msg off, a reply
// routed through a stored conversation link while the config cannot be read
// must go out without the agent's name, not with the global setting.
func TestAgentReplyIsUnsignedWhenSignatureLookupFails(t *testing.T) {
	withSignConfig(t, true, "\n\n")

	off := false
	repo := &failingConfigRepo{signTestRepo: signTestRepo{
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
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if repo.lookups == 0 {
		t.Fatal("the test must exercise a failing config lookup")
	}
	if len(sender.sent) != 1 || sender.sent[0] != "Hello" {
		t.Fatalf("sent text = %q, want [\"Hello\"] (unsigned)", sender.sent)
	}
}
