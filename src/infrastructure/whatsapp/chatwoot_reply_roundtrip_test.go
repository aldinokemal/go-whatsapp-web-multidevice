package whatsapp_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	domain "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

const (
	contactPhone = "5500912345678"
	deliveredJID = "550012345678@s.whatsapp.net"
)

// At the WhatsApp boundary, the send succeeds under the registered spelling.
type deliveredReplySender struct{ send.ISendUsecase }

func (deliveredReplySender) SendText(_ context.Context, r send.MessageRequest) (send.GenericResponse, error) {
	return deliveredReply(r.Phone)
}

func (deliveredReplySender) SendImage(_ context.Context, r send.ImageRequest) (send.GenericResponse, error) {
	return deliveredReply(r.Phone)
}

func deliveredReply(phone string) (send.GenericResponse, error) {
	if phone != contactPhone {
		return send.GenericResponse{}, fmt.Errorf("unexpected send destination %q", phone)
	}
	return send.GenericResponse{MessageID: "agent-wa", RecipientJID: deliveredJID}, nil
}

func TestChatwootReplyRoundTripWithAlternatePhoneSpelling(t *testing.T) {
	for _, tc := range []struct{ name, attachments string }{
		{"text", "[]"},
		{"image", `[{"file_type":"image","data_url":"https://example.test/image.png"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open(sqlite.DriverName, filepath.Join(t.TempDir(), "chat.db"))
			require.NoError(t, err)
			t.Cleanup(func() { db.Close() })
			repo := chatstorage.NewStorageRepository(db)
			require.NoError(t, repo.InitializeSchema())

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var response any
				switch r.Method + " " + r.URL.Path {
				case "GET /api/v1/accounts/1/contacts/search":
					response = map[string]any{"payload": []chatwoot.Contact{{ID: 7, PhoneNumber: "+" + contactPhone}}}
				case "GET /api/v1/accounts/1/conversations/42":
					response = chatwoot.ConversationWebhook{ID: 42, InboxID: 2, Meta: chatwoot.ConversationMeta{Sender: chatwoot.Contact{ID: 7, Name: "Customer"}}}
				case "GET /api/v1/accounts/1/contacts/7/conversations":
					response = map[string]any{"payload": []chatwoot.Conversation{{ID: 42, InboxID: 2, Status: "open"}}}
				case "POST /api/v1/accounts/1/conversations/42/messages":
					response = chatwoot.Message{ID: 73}
				default:
					t.Errorf("unexpected Chatwoot request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if err := json.NewEncoder(w).Encode(response); err != nil {
					t.Errorf("encode Chatwoot response: %v", err)
				}
			}))
			t.Cleanup(server.Close)
			const device = "slot-a"
			cfg := &domain.ChatwootDeviceConfig{
				DeviceID: device, ChatwootURL: server.URL, AccountID: 1, InboxID: 2,
				APIToken: "test", WebhookSecret: "test", Enabled: true,
			}
			require.NoError(t, repo.SaveChatwootDeviceConfig(cfg))
			chatwoot.InitClientRegistry(repo)
			t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })
			rc, err := chatwoot.GetClientRegistry().Resolve(device)
			require.NoError(t, err)
			require.NotNil(t, rc)
			rc.Client.HTTPClient = server.Client()

			dm := whatsapp.NewDeviceManager(nil, nil, nil)
			dm.AddDevice(whatsapp.NewDeviceInstance(device, nil, repo))
			handler := rest.NewChatwootHandler(nil, deliveredReplySender{}, nil, dm, repo)
			app := fiber.New()
			app.Post("/chatwoot/webhook", handler.HandleWebhook)
			body := fmt.Sprintf(`{
				"event":"message_created", "message_type":"outgoing", "id":72,
				"content":"hello", "account":{"id":1}, "attachments":%s,
				"conversation":{"id":42, "inbox_id":2, "meta":{"sender":{"phone_number":"+%s"}}}
			}`, tc.attachments, contactPhone)
			req := httptest.NewRequest(http.MethodPost, "/chatwoot/webhook", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Chatwoot-Webhook-Secret", "test")
			resp, err := app.Test(req)
			require.NoError(t, err)
			resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)
			link, err := repo.GetChatwootMessageLinkByWhatsAppID(device, "agent-wa")
			require.NoError(t, err)
			require.NotNil(t, link)
			require.Equal(t, deliveredJID, link.WhatsAppChatJID)

			reply := map[string]any{"payload": map[string]any{
				"id": "customer-wa", "from": deliveredJID, "chat_id": deliveredJID, "body": "reply",
			}}
			require.NoError(t, whatsapp.SyncPayloadToChatwootForTest(t.Context(), reply, "message", device, repo))
			inbound, err := repo.GetChatwootMessageLinkByWhatsAppID(device, "customer-wa")
			require.NoError(t, err)
			require.NotNil(t, inbound)
			require.Equal(t, 42, inbound.ChatwootConversationID)
			require.Equal(t, 73, inbound.ChatwootMessageID)
		})
	}
}
