package whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func companionInstance(slot, user string, device uint8) *DeviceInstance {
	jid := types.NewADJID(user, 0, device)
	return NewDeviceInstance(slot, &whatsmeow.Client{Store: &store.Device{ID: &jid}}, nil)
}

func TestWebhookPolicyUsesCompanionSlot(t *testing.T) {
	origHooks := config.WhatsappWebhook
	origEvents := config.WhatsappWebhookEvents
	origStorage := webhookStorageForTest
	origSubmit := submitWebhookFn
	t.Cleanup(func() {
		config.WhatsappWebhook = origHooks
		config.WhatsappWebhookEvents = origEvents
		webhookStorageForTest = origStorage
		submitWebhookFn = origSubmit
	})

	slot := companionInstance("slot-a", "6281777000021", 28)
	bare := slot.JID()
	ad := slot.ADJID()
	slotURL := "https://slot.example/hook"
	config.WhatsappWebhook = []string{"https://global.example/hook"}
	config.WhatsappWebhookEvents = nil
	webhookStorageForTest = func(key string) (*chatstorage.DeviceRecord, error) {
		if key != ad {
			return nil, nil
		}
		return &chatstorage.DeviceRecord{
			DeviceID:      "slot-a",
			JID:           bare,
			ADJID:         ad,
			WebhookURL:    &slotURL,
			WebhookEvents: "message",
		}, nil
	}

	var urls []string
	submitWebhookFn = func(_ context.Context, _ map[string]any, url string, _ *chatstorage.DeviceWebhookConfig) error {
		urls = append(urls, url)
		return nil
	}

	ctx := ContextWithDevice(context.Background(), slot)
	payload := map[string]any{"device_id": bare, "payload": map[string]any{}}

	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "newsletter.joined"); err != nil {
		t.Fatalf("forward: %v", err)
	}
	if len(urls) != 0 {
		t.Fatalf("slot policy excludes newsletter.joined, delivered to %v", urls)
	}

	urls = nil
	if err := forwardPayloadToConfiguredWebhooks(ctx, payload, "message"); err != nil {
		t.Fatalf("forward message: %v", err)
	}
	if len(urls) != 1 || urls[0] != slotURL {
		t.Fatalf("message delivered to %v, want slot webhook", urls)
	}
}

func TestWebhookPolicyStopsWhenBareJIDIsAmbiguous(t *testing.T) {
	origHooks := config.WhatsappWebhook
	origDM := deviceManager
	origStorage := webhookStorageForTest
	origSubmit := submitWebhookFn
	t.Cleanup(func() {
		config.WhatsappWebhook = origHooks
		deviceManager = origDM
		webhookStorageForTest = origStorage
		submitWebhookFn = origSubmit
	})

	config.WhatsappWebhook = []string{"https://global.example/hook"}
	deviceManager = NewDeviceManager(nil, nil, nil)
	slotA := companionInstance("slot-a", "6281777000021", 28)
	deviceManager.AddDevice(slotA)
	deviceManager.AddDevice(companionInstance("slot-b", "6281777000021", 32))
	webhookStorageForTest = func(string) (*chatstorage.DeviceRecord, error) { return nil, nil }

	called := false
	submitWebhookFn = func(context.Context, map[string]any, string, *chatstorage.DeviceWebhookConfig) error {
		called = true
		return nil
	}

	err := forwardPayloadToConfiguredWebhooks(context.Background(), map[string]any{
		"device_id": slotA.JID(),
		"payload":   map[string]any{},
	}, "newsletter.joined")
	if err != nil {
		t.Fatalf("ambiguous delivery should skip, not fail the caller: %v", err)
	}
	if called {
		t.Fatal("ambiguous bare account JID used the global webhook policy")
	}
}

func TestRetryFailsWhenQueuedDeviceIsGone(t *testing.T) {
	origDM := deviceManager
	origName := contactDisplayNameFn
	t.Cleanup(func() {
		deviceManager = origDM
		contactDisplayNameFn = origName
	})
	deviceManager = NewDeviceManager(nil, nil, nil)
	deviceManager.AddDevice(companionInstance("other", "6281999999999", 4))

	called := false
	contactDisplayNameFn = func(context.Context, string) string {
		called = true
		return "from the other account"
	}

	err := processChatwootForwardRetryEvent(&chatwootForwardQueueTestRepo{}, &chatstorage.ChatwootForwardEvent{
		ID:          1,
		DeviceID:    "missing-slot",
		EventName:   "message",
		PayloadJSON: `{"payload":{"id":"m1","from":"6281@s.whatsapp.net","chat_id":"6281@s.whatsapp.net","body":"hi"}}`,
	})
	if err == nil {
		t.Fatal("retry should fail when the queued device cannot be restored")
	}
	if called {
		t.Fatal("retry read a contact name without the queued device")
	}

	deviceManager = nil
	if err := processChatwootForwardRetryEvent(&chatwootForwardQueueTestRepo{}, &chatstorage.ChatwootForwardEvent{
		ID:          2,
		DeviceID:    "missing-slot",
		EventName:   "message",
		PayloadJSON: `{"payload":{"id":"m1"}}`,
	}); err == nil {
		t.Fatal("retry should fail when no device manager can restore the device")
	}
}

type authzLinkRepo struct {
	chatstorage.IChatStorageRepository
	link *chatstorage.ChatwootMessageLink
	msg  *chatstorage.Message
}

func (r *authzLinkRepo) GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID string) (*chatstorage.ChatwootMessageLink, error) {
	if r.link != nil && r.link.DeviceID == deviceID && r.link.WhatsAppMessageID == waMessageID {
		cloned := *r.link
		return &cloned, nil
	}
	return nil, nil
}

func (r *authzLinkRepo) GetMessageByIDChatAndDevice(deviceID, chatJID, id string) (*chatstorage.Message, error) {
	if r.msg != nil && r.msg.DeviceID == deviceID && r.msg.ChatJID == chatJID && r.msg.ID == id {
		cloned := *r.msg
		return &cloned, nil
	}
	return nil, nil
}

func TestChatwootLinkRequiresConfigurationNotJustAccount(t *testing.T) {
	cw := &chatwoot.Client{AccountID: 1}
	sameAccountOtherServer := &chatstorage.ChatwootMessageLink{ChatwootAccountID: 1, ChatwootConfigID: 5}
	if chatwootLinkMatchesClient(sameAccountOtherServer, cw, 9) {
		t.Fatal("account number collision was treated as the same Chatwoot server")
	}
	if !chatwootLinkMatchesClient(sameAccountOtherServer, cw, 5) {
		t.Fatal("matching configuration was rejected")
	}
	legacy := &chatstorage.ChatwootMessageLink{ChatwootAccountID: 1, ChatwootConfigID: 0}
	if chatwootLinkMatchesClient(legacy, cw, 5) {
		t.Fatal("legacy link matched a per-device configuration")
	}
	if !chatwootLinkMatchesClient(legacy, cw, 0) {
		t.Fatal("legacy client should accept a legacy link for the same account")
	}
}

func TestChatwootExclusionRunsBeforeReceiptAndDelete(t *testing.T) {
	origIgnore := config.ChatwootIgnoreJids
	origDelete := config.ChatwootMessageDelete
	t.Cleanup(func() {
		config.ChatwootIgnoreJids = origIgnore
		config.ChatwootMessageDelete = origDelete
	})
	config.ChatwootIgnoreJids = []string{"111222333@lid", "628555@s.whatsapp.net"}
	config.ChatwootMessageDelete = true

	_, err := extractChatwootContactInfo(context.Background(), map[string]any{
		"from":     "628123@s.whatsapp.net",
		"chat_id":  "628123@s.whatsapp.net",
		"from_lid": "111222333@lid",
	})
	if err == nil {
		t.Fatal("resolved phone JID bypassed an excluded LID alias")
	}

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	cw := &chatwoot.Client{BaseURL: srv.URL, APIToken: "t", AccountID: 1, InboxID: 2, HTTPClient: srv.Client()}
	repo := &authzLinkRepo{link: &chatstorage.ChatwootMessageLink{
		DeviceID:                     "dev",
		WhatsAppMessageID:            "m1",
		WhatsAppChatJID:              "628123@s.whatsapp.net",
		ChatwootMessageID:            8,
		ChatwootConversationID:       9,
		ChatwootContactInboxSourceID: "src",
		ChatwootConfigID:             3,
		ChatwootAccountID:            1,
	}}

	syncReadReceiptsToChatwoot(cw, "dev", repo, 3, map[string]any{
		"receipt_type": "read",
		"ids":          []string{"m1"},
		"chat_id":      "628123@s.whatsapp.net",
		"chat_lid":     "111222333@lid",
	})
	if hit {
		t.Fatal("read receipt updated an excluded chat")
	}

	hit = false
	repo.link.WhatsAppChatJID = "628555@s.whatsapp.net"
	if deleteLinkedChatwootMessage(cw, "dev", repo, 3, "message.revoked", map[string]any{
		"revoked_message_id": "m1",
		"chat_id":            "628555@s.whatsapp.net",
		"from":               "628555@s.whatsapp.net",
	}) {
		t.Fatal("delete ran for an excluded chat")
	}
	if hit {
		t.Fatal("excluded chat was deleted in Chatwoot")
	}
}

func TestChatwootRevokeRequiresAuthor(t *testing.T) {
	origDelete := config.ChatwootMessageDelete
	origIgnore := config.ChatwootIgnoreJids
	t.Cleanup(func() {
		config.ChatwootMessageDelete = origDelete
		config.ChatwootIgnoreJids = origIgnore
	})
	config.ChatwootMessageDelete = true
	config.ChatwootIgnoreJids = nil

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	cw := &chatwoot.Client{BaseURL: srv.URL, APIToken: "t", AccountID: 1, InboxID: 2, HTTPClient: srv.Client()}
	chatJID := "628123@s.whatsapp.net"
	repo := &authzLinkRepo{
		link: &chatstorage.ChatwootMessageLink{
			DeviceID:               "dev",
			WhatsAppMessageID:      "m1",
			WhatsAppChatJID:        chatJID,
			ChatwootMessageID:      8,
			ChatwootConversationID: 9,
			ChatwootConfigID:       3,
			ChatwootAccountID:      1,
		},
		msg: &chatstorage.Message{
			ID: "m1", ChatJID: chatJID, DeviceID: "dev", Sender: "628123@s.whatsapp.net",
		},
	}
	data := map[string]any{
		"revoked_message_id": "m1",
		"chat_id":            chatJID,
		"from":               "628999@s.whatsapp.net",
		"is_from_me":         false,
	}
	if deleteLinkedChatwootMessage(cw, "dev", repo, 3, "message.revoked", data) {
		t.Fatal("unauthorized revoke deleted the Chatwoot message")
	}
	if hit {
		t.Fatal("Chatwoot was called for an unauthorized revoke")
	}

	NoteMessageRevokeAuthorization("dev", chatJID, "m1", true)
	t.Cleanup(func() { revokeAuthNotes.Delete(revokeAuthKey("dev", chatJID, "m1")) })
	data["from"] = "628123@s.whatsapp.net"
	if !deleteLinkedChatwootMessage(cw, "dev", repo, 3, "message.revoked", data) {
		t.Fatal("authorized revoke was not forwarded")
	}
	if !hit {
		t.Fatal("authorized revoke did not reach Chatwoot")
	}
}

func TestMessageAuthorMatchesRejectsEmptyStoredSender(t *testing.T) {
	if MessageAuthorMatches("", false, "6281@s.whatsapp.net", "6281@s.whatsapp.net", false) {
		t.Fatal("empty stored sender matched a peer")
	}
	if !MessageAuthorMatches("628000@s.whatsapp.net", true, "628999@s.whatsapp.net", "", true) {
		t.Fatal("same-account IsFromMe edit should match")
	}
}

func TestSyncPayloadSkipsUnauthorizedEditAndRevoke(t *testing.T) {
	origEdit := config.ChatwootForwardEdits
	origDelete := config.ChatwootForwardDeletes
	origHard := config.ChatwootMessageDelete
	origClient := getChatwootClientFn
	t.Cleanup(func() {
		config.ChatwootForwardEdits = origEdit
		config.ChatwootForwardDeletes = origDelete
		config.ChatwootMessageDelete = origHard
		getChatwootClientFn = origClient
		for _, id := range []string{"EDIT-FOREIGN", "REV-FOREIGN", "EDIT-OWN"} {
			messageEventAuthNotes.Delete(messageEventAuthKey("dev", id))
		}
	})
	config.ChatwootForwardEdits = true
	config.ChatwootForwardDeletes = true
	config.ChatwootMessageDelete = true

	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"payload":[]}`))
	}))
	defer srv.Close()
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) {
		return &chatwoot.ResolvedConfig{
			ConfigID: 3,
			Client: &chatwoot.Client{
				BaseURL: srv.URL, APIToken: "t", AccountID: 1, InboxID: 2, HTTPClient: srv.Client(),
			},
		}, nil
	}
	repo := &authzLinkRepo{link: &chatstorage.ChatwootMessageLink{
		DeviceID: "dev", WhatsAppMessageID: "m1", WhatsAppChatJID: "628123@s.whatsapp.net",
		ChatwootMessageID: 8, ChatwootConversationID: 9, ChatwootConfigID: 3, ChatwootAccountID: 1,
		ChatwootContactInboxSourceID: "src",
	}}
	chatID := "628123@s.whatsapp.net"

	NoteMessageEventAuthorization("dev", "EDIT-FOREIGN", false)
	err := syncPayloadToChatwoot(context.Background(), map[string]any{"payload": map[string]any{
		"id": "EDIT-FOREIGN", "original_message_id": "m1", "body": "pwned",
		"from": "628000@s.whatsapp.net", "chat_id": chatID, "is_from_me": false,
	}}, "message.edited", "dev", repo)
	if err != nil {
		t.Fatalf("unauthorized edit: %v", err)
	}
	if hit {
		t.Fatal("unauthorized edit wrote to Chatwoot")
	}

	NoteMessageEventAuthorization("dev", "REV-FOREIGN", false)
	err = syncPayloadToChatwoot(context.Background(), map[string]any{"payload": map[string]any{
		"id": "REV-FOREIGN", "revoked_message_id": "m1",
		"from": "628000@s.whatsapp.net", "chat_id": chatID, "is_from_me": false,
	}}, "message.revoked", "dev", repo)
	if err != nil {
		t.Fatalf("unauthorized revoke: %v", err)
	}
	if hit {
		t.Fatal("unauthorized revoke wrote to Chatwoot")
	}

	NoteMessageEventAuthorization("dev", "EDIT-OWN", true)
	err = syncPayloadToChatwoot(context.Background(), map[string]any{"payload": map[string]any{
		"id": "EDIT-OWN", "original_message_id": "m1", "body": "edited by us",
		"from": "628123@s.whatsapp.net", "chat_id": chatID, "is_from_me": true,
	}}, "message.edited", "dev", repo)
	if !hit {
		t.Fatalf("same-account edit was not forwarded (err=%v)", err)
	}
}

func TestDeleteAndReceiptPayloadsCarryLIDForExclusion(t *testing.T) {
	origIgnore := config.ChatwootIgnoreJids
	origDelete := config.ChatwootMessageDelete
	origForward := config.ChatwootForwardDeletes
	origClient := getChatwootClientFn
	t.Cleanup(func() {
		config.ChatwootIgnoreJids = origIgnore
		config.ChatwootMessageDelete = origDelete
		config.ChatwootForwardDeletes = origForward
		getChatwootClientFn = origClient
	})
	config.ChatwootIgnoreJids = []string{"111@lid"}
	config.ChatwootMessageDelete = true
	config.ChatwootForwardDeletes = true

	phone := types.NewJID("1555121200", types.DefaultUserServer)
	lid := types.NewJID("111", types.HiddenUserServer)
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) {
		return &chatwoot.ResolvedConfig{
			ConfigID: 3,
			Client: &chatwoot.Client{
				BaseURL: srv.URL, APIToken: "t", AccountID: 1, InboxID: 2, HTTPClient: srv.Client(),
			},
		}, nil
	}
	repo := &authzLinkRepo{link: &chatstorage.ChatwootMessageLink{
		DeviceID: "dev", WhatsAppMessageID: "m1", WhatsAppChatJID: phone.String(),
		ChatwootMessageID: 8, ChatwootConversationID: 9, ChatwootConfigID: 3, ChatwootAccountID: 1,
		ChatwootContactInboxSourceID: "src",
	}}

	deleteBody, err := createDeletePayload(context.Background(), &events.DeleteForMe{
		ChatJID: lid, SenderJID: phone, MessageID: "m1", Timestamp: time.Now(),
	}, &chatstorage.Message{
		ChatJID: phone.String(), Sender: phone.String(), Content: "secret", Timestamp: time.Now(),
	}, "dev", nil)
	if err != nil {
		t.Fatalf("createDeletePayload: %v", err)
	}
	inner, _ := deleteBody["payload"].(map[string]any)
	if inner["chat_id"] != phone.String() || inner["chat_lid"] != "111@lid" {
		t.Fatalf("delete payload lost the original LID: %#v", inner)
	}
	if err := syncPayloadToChatwoot(context.Background(), deleteBody, "message.deleted", "dev", repo); err != nil {
		t.Fatalf("delete sync: %v", err)
	}
	if hit {
		t.Fatal("delete-for-me of an ignored LID updated Chatwoot")
	}

	receipt := createReceiptPayload(context.Background(), &events.Receipt{
		MessageSource: types.MessageSource{
			Chat: phone, Sender: phone, RecipientAlt: lid,
		},
		MessageIDs: []types.MessageID{"m1"},
		Type:       types.ReceiptTypeRead,
		Timestamp:  time.Now(),
	}, "dev", nil)
	inner, _ = receipt["payload"].(map[string]any)
	if inner["chat_id"] != phone.String() || inner["chat_lid"] != "111@lid" {
		t.Fatalf("receipt payload lost the alt LID: %#v", inner)
	}
	if err := syncPayloadToChatwoot(context.Background(), receipt, "message.ack", "dev", repo); err != nil {
		t.Fatalf("receipt sync: %v", err)
	}
	if hit {
		t.Fatal("read receipt for an ignored LID updated last seen")
	}
}
