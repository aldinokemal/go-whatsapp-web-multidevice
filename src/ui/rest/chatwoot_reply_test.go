package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/gofiber/fiber/v3"
)

// chatwootReplyTestRepo backs the agent-reply delivery path in legacy mode (no
// per-device configs, no conversation link). GetChatwootMessageLinkByChatwootID
// keeps the real repo's device scope, so a handler that passed the wrong device
// would get nil here.
type chatwootReplyTestRepo struct {
	domainChatStorage.IChatStorageRepository
	links   []domainChatStorage.ChatwootMessageLink
	linkErr error
}

func (r *chatwootReplyTestRepo) CountChatwootDeviceConfigs() (int, error) {
	return 0, nil
}

func (r *chatwootReplyTestRepo) GetLatestChatwootMessageLinkByConversation(int, int, bool, int64) (*domainChatStorage.ChatwootMessageLink, error) {
	return nil, nil
}

func (r *chatwootReplyTestRepo) GetChatwootMessageLinkByChatwootID(deviceID string, chatwootMessageID int) (*domainChatStorage.ChatwootMessageLink, error) {
	if r.linkErr != nil {
		return nil, r.linkErr
	}
	for _, link := range r.links {
		if link.DeviceID == deviceID && link.ChatwootMessageID == chatwootMessageID {
			cloned := link
			return &cloned, nil
		}
	}
	return nil, nil
}

func (r *chatwootReplyTestRepo) UpsertChatwootMessageLink(*domainChatStorage.ChatwootMessageLink) error {
	return nil
}

type chatwootSentMessage struct {
	kind string
	// reply is the ReplyMessageID the send carried; "" means none.
	reply string
}

// chatwootReplySendStub records each send the webhook issues, in order. Audio
// sends fail so the file fallback is exercised as well.
type chatwootReplySendStub struct {
	domainSend.ISendUsecase
	sent []chatwootSentMessage
}

func (s *chatwootReplySendStub) record(kind string, reply *string) domainSend.GenericResponse {
	msg := chatwootSentMessage{kind: kind}
	if reply != nil {
		msg.reply = *reply
	}
	s.sent = append(s.sent, msg)
	return domainSend.GenericResponse{MessageID: "wa-out", Status: "ok"}
}

func (s *chatwootReplySendStub) SendText(_ context.Context, req domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	return s.record("text", req.ReplyMessageID), nil
}

func (s *chatwootReplySendStub) SendImage(_ context.Context, req domainSend.ImageRequest) (domainSend.GenericResponse, error) {
	return s.record("image", req.ReplyMessageID), nil
}

func (s *chatwootReplySendStub) SendFile(_ context.Context, req domainSend.FileRequest) (domainSend.GenericResponse, error) {
	return s.record("file", req.ReplyMessageID), nil
}

func (s *chatwootReplySendStub) SendAudio(_ context.Context, req domainSend.AudioRequest) (domainSend.GenericResponse, error) {
	s.record("audio", req.ReplyMessageID)
	return domainSend.GenericResponse{}, errors.New("audio rejected")
}

// TestChatwootWebhookQuotesRepliedMessage covers agent replies made with
// Chatwoot's "Reply" action: Chatwoot puts the quoted message's id in
// content_attributes.in_reply_to and its source_id in in_reply_to_external_id.
// The WhatsApp send must quote that message when it can be resolved, and go out
// unquoted otherwise.
func TestChatwootWebhookQuotesRepliedMessage(t *testing.T) {
	chatwoot.InitClientRegistry(nil)
	t.Cleanup(func() { chatwoot.InitClientRegistry(nil) })

	const (
		accountID      = 1
		conversationID = 7
	)
	agentReplyLink := func(deviceID string, account, conversation int) domainChatStorage.ChatwootMessageLink {
		return domainChatStorage.ChatwootMessageLink{
			DeviceID:               deviceID,
			WhatsAppMessageID:      "3EB0B2",
			ChatwootMessageID:      42,
			ChatwootConversationID: conversation,
			ChatwootAccountID:      account,
			Direction:              "outgoing",
		}
	}
	image := map[string]any{"id": 1, "file_type": "image", "data_url": "https://chat.example.com/a.jpg"}
	file := map[string]any{"id": 2, "file_type": "file", "data_url": "https://chat.example.com/b.pdf"}
	audio := map[string]any{"id": 3, "file_type": "audio", "data_url": "https://chat.example.com/c.ogg"}

	tests := []struct {
		name              string
		contentAttributes map[string]any
		attachments       []map[string]any
		links             []domainChatStorage.ChatwootMessageLink
		linkErr           error
		want              []chatwootSentMessage
	}{
		{
			name:              "WAID external id quotes the text reply",
			contentAttributes: map[string]any{"in_reply_to": 42, "in_reply_to_external_id": "WAID:3EB0A1"},
			want:              []chatwootSentMessage{{kind: "text", reply: "3EB0A1"}},
		},
		{
			name: "no content_attributes sends unquoted",
			want: []chatwootSentMessage{{kind: "text"}},
		},
		{
			name:              "external id without WAID prefix is ignored",
			contentAttributes: map[string]any{"in_reply_to_external_id": "3EB0A1"},
			want:              []chatwootSentMessage{{kind: "text"}},
		},
		{
			name:              "empty WAID id is ignored",
			contentAttributes: map[string]any{"in_reply_to_external_id": "WAID:"},
			want:              []chatwootSentMessage{{kind: "text"}},
		},
		{
			// Quoting an earlier agent reply: it has no source_id, so Chatwoot
			// sends a null external id and the outbound link maps the id.
			name:              "quoted agent reply resolves through the outbound link",
			contentAttributes: map[string]any{"in_reply_to": 42, "in_reply_to_external_id": nil},
			links:             []domainChatStorage.ChatwootMessageLink{agentReplyLink("dev", accountID, conversationID)},
			want:              []chatwootSentMessage{{kind: "text", reply: "3EB0B2"}},
		},
		{
			// Rows written before links carried the account id hold 0.
			name:              "legacy outbound link without account resolves",
			contentAttributes: map[string]any{"in_reply_to": 42, "in_reply_to_external_id": nil},
			links:             []domainChatStorage.ChatwootMessageLink{agentReplyLink("dev", 0, conversationID)},
			want:              []chatwootSentMessage{{kind: "text", reply: "3EB0B2"}},
		},
		{
			name:              "outbound link from another conversation is ignored",
			contentAttributes: map[string]any{"in_reply_to": 42, "in_reply_to_external_id": nil},
			links:             []domainChatStorage.ChatwootMessageLink{agentReplyLink("dev", accountID, conversationID+1)},
			want:              []chatwootSentMessage{{kind: "text"}},
		},
		{
			name:              "outbound link from another account is ignored",
			contentAttributes: map[string]any{"in_reply_to": 42, "in_reply_to_external_id": nil},
			links:             []domainChatStorage.ChatwootMessageLink{agentReplyLink("dev", accountID+1, conversationID)},
			want:              []chatwootSentMessage{{kind: "text"}},
		},
		{
			name:              "outbound link from another device is ignored",
			contentAttributes: map[string]any{"in_reply_to": 42, "in_reply_to_external_id": nil},
			links:             []domainChatStorage.ChatwootMessageLink{agentReplyLink("other", accountID, conversationID)},
			want:              []chatwootSentMessage{{kind: "text"}},
		},
		{
			name:              "link lookup error still sends, unquoted",
			contentAttributes: map[string]any{"in_reply_to": 42, "in_reply_to_external_id": nil},
			linkErr:           errors.New("storage unavailable"),
			want:              []chatwootSentMessage{{kind: "text"}},
		},
		{
			name:              "quote rides on the first attachment only",
			contentAttributes: map[string]any{"in_reply_to": 42, "in_reply_to_external_id": "WAID:3EB0A1"},
			attachments:       []map[string]any{image, file},
			want:              []chatwootSentMessage{{kind: "image", reply: "3EB0A1"}, {kind: "file"}},
		},
		{
			name:              "audio file fallback keeps the quote",
			contentAttributes: map[string]any{"in_reply_to": 42, "in_reply_to_external_id": "WAID:3EB0A1"},
			attachments:       []map[string]any{audio},
			want:              []chatwootSentMessage{{kind: "audio", reply: "3EB0A1"}, {kind: "file", reply: "3EB0A1"}},
		},
		{
			name:        "attachment without a quoted message stays unquoted",
			attachments: []map[string]any{image},
			want:        []chatwootSentMessage{{kind: "image"}},
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dm := whatsapp.NewDeviceManager(nil, nil, nil)
			dm.AddDevice(whatsapp.NewDeviceInstance("dev", nil, nil))
			stub := &chatwootReplySendStub{}
			handler := &ChatwootHandler{
				SendUsecase:     stub,
				DeviceManager:   dm,
				ChatStorageRepo: &chatwootReplyTestRepo{links: tt.links, linkErr: tt.linkErr},
			}
			app := fiber.New()
			app.Post("/chatwoot/webhook", handler.HandleWebhook)

			payload := map[string]any{
				"id":           1000 + i,
				"event":        "message_created",
				"message_type": "outgoing",
				"content":      "hello",
				"account":      map[string]any{"id": accountID},
				"conversation": map[string]any{
					"id":       conversationID,
					"inbox_id": 2,
					"meta": map[string]any{"sender": map[string]any{
						"custom_attributes": map[string]any{
							"gowa_device_id":    "dev",
							"gowa_whatsapp_jid": "100000000000001@lid",
						},
					}},
				},
			}
			if tt.contentAttributes != nil {
				payload["content_attributes"] = tt.contentAttributes
			}
			if tt.attachments != nil {
				payload["attachments"] = tt.attachments
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/chatwoot/webhook", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusOK)
			}
			if len(stub.sent) != len(tt.want) {
				t.Fatalf("sent = %+v, want %+v", stub.sent, tt.want)
			}
			for j := range tt.want {
				if stub.sent[j] != tt.want[j] {
					t.Fatalf("sent[%d] = %+v, want %+v", j, stub.sent[j], tt.want[j])
				}
			}
		})
	}
}
