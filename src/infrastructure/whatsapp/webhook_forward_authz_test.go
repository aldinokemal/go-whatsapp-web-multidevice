package whatsapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	origEnabled := config.ChatwootEnabled
	origDelete := config.ChatwootMessageDelete
	origIgnore := config.ChatwootIgnoreJids
	t.Cleanup(func() {
		config.ChatwootEnabled = origEnabled
		config.ChatwootMessageDelete = origDelete
		config.ChatwootIgnoreJids = origIgnore
	})
	config.ChatwootEnabled = true
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
	origEnabled := config.ChatwootEnabled
	origEdit := config.ChatwootForwardEdits
	origDelete := config.ChatwootForwardDeletes
	origHard := config.ChatwootMessageDelete
	origClient := getChatwootClientFn
	t.Cleanup(func() {
		config.ChatwootEnabled = origEnabled
		config.ChatwootForwardEdits = origEdit
		config.ChatwootForwardDeletes = origDelete
		config.ChatwootMessageDelete = origHard
		getChatwootClientFn = origClient
		for _, id := range []string{"EDIT-FOREIGN", "REV-FOREIGN", "EDIT-OWN"} {
			messageEventAuthNotes.Delete(messageEventAuthKey("dev", id))
		}
	})
	config.ChatwootEnabled = true
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

	// No decision was recorded (storage failed or never saw the event): a
	// peer's edit or revoke is denied.
	for _, event := range []string{"message.edited", "message.revoked"} {
		err = syncPayloadToChatwoot(context.Background(), map[string]any{"payload": map[string]any{
			"id": "PEER-UNDECIDED", "original_message_id": "m1", "revoked_message_id": "m1", "body": "pwned",
			"from": "628000@s.whatsapp.net", "chat_id": chatID, "is_from_me": false,
		}}, event, "dev", repo)
		if err != nil {
			t.Fatalf("undecided peer %s: %v", event, err)
		}
		if hit {
			t.Fatalf("undecided peer %s wrote to Chatwoot", event)
		}
	}

	NoteMessageEventAuthorization("dev", "EDIT-OWN", true)
	err = syncPayloadToChatwoot(context.Background(), map[string]any{"payload": map[string]any{
		"id": "EDIT-OWN", "original_message_id": "m1", "body": "edited by us",
		"from": "628123@s.whatsapp.net", "chat_id": chatID, "is_from_me": true,
	}}, "message.edited", "dev", repo)
	if !hit {
		t.Fatalf("same-account edit was not forwarded (err=%v)", err)
	}

	// This account may edit or revoke what it sent without a recorded decision.
	for _, event := range []string{"message.edited", "message.revoked"} {
		hit = false
		err = syncPayloadToChatwoot(context.Background(), map[string]any{"payload": map[string]any{
			"id": "OWN-UNDECIDED", "original_message_id": "m1", "revoked_message_id": "m1", "body": "edited by us",
			"from": "628123@s.whatsapp.net", "chat_id": chatID, "is_from_me": true,
		}}, event, "dev", repo)
		if !hit {
			t.Fatalf("undecided same-account %s was not forwarded (err=%v)", event, err)
		}
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

func TestIsGroupAdminMatchesLIDOrPhone(t *testing.T) {
	orig := fetchGroupInfoFn
	t.Cleanup(func() { fetchGroupInfoFn = orig })
	group := types.NewJID("120363000000000001", types.GroupServer)
	adminLID := types.NewJID("111", types.HiddenUserServer)
	adminPN := types.NewJID("628111", types.DefaultUserServer)
	memberLID := types.NewJID("222", types.HiddenUserServer)
	var lookupErr error
	fetchGroupInfoFn = func(_ context.Context, _ *whatsmeow.Client, jid types.JID) (*types.GroupInfo, error) {
		if lookupErr != nil || jid != group {
			return nil, lookupErr
		}
		return &types.GroupInfo{Participants: []types.GroupParticipant{
			{JID: adminLID, PhoneNumber: adminPN, IsAdmin: true},
			{JID: memberLID, PhoneNumber: types.NewJID("628222", types.DefaultUserServer)},
		}}, nil
	}
	ctx := context.Background()
	client := &whatsmeow.Client{}

	companion := adminLID
	companion.Device = 3
	if !IsGroupAdmin(ctx, client, group, companion) {
		t.Fatal("admin sending from a companion device was not recognized by LID")
	}
	if !IsGroupAdmin(ctx, client, group, adminPN, types.EmptyJID) {
		t.Fatal("admin addressed by phone in a LID group was not recognized")
	}
	if IsGroupAdmin(ctx, client, group, memberLID, types.EmptyJID) {
		t.Fatal("a member who is not an admin was treated as one")
	}
	if IsGroupAdmin(ctx, nil, group, adminLID) {
		t.Fatal("a missing client answered as admin")
	}
	lookupErr = errors.New("timeout")
	if IsGroupAdmin(ctx, client, group, adminLID) {
		t.Fatal("a failed group lookup was treated as admin")
	}
}

func TestAuthNotesNeedChatwootAndExpiredOnesAreSwept(t *testing.T) {
	origEnabled := config.ChatwootEnabled
	t.Cleanup(func() {
		config.ChatwootEnabled = origEnabled
		ForgetMessageAuthorizationsForTest("dev-sweep")
	})
	chatJID := "628123@s.whatsapp.net"

	config.ChatwootEnabled = false
	NoteMessageEventAuthorization("dev-sweep", "EVT-OFF", false)
	NoteMessageRevokeAuthorization("dev-sweep", chatJID, "m-off", false)
	if _, ok := MessageEventAuthorized("dev-sweep", "EVT-OFF"); ok {
		t.Fatal("event decision kept while Chatwoot is disabled")
	}
	if _, ok := MessageRevokeAuthorized("dev-sweep", chatJID, "m-off"); ok {
		t.Fatal("revoke decision kept while Chatwoot is disabled")
	}

	config.ChatwootEnabled = true
	staleEvent := messageEventAuthKey("dev-sweep", "EVT-STALE")
	staleRevoke := revokeAuthKey("dev-sweep", chatJID, "m-stale")
	expired := revokeAuthRecord{expires: time.Now().Add(-time.Minute)}
	messageEventAuthNotes.Store(staleEvent, expired)
	revokeAuthNotes.Store(staleRevoke, expired)
	authNotesSweptAt.Store(0)
	NoteMessageEventAuthorization("dev-sweep", "EVT-NEW", true)
	if _, found := messageEventAuthNotes.Load(staleEvent); found {
		t.Fatal("expired event decision survived the sweep")
	}
	if _, found := revokeAuthNotes.Load(staleRevoke); found {
		t.Fatal("expired revoke decision survived the sweep")
	}
	if authorized, ok := MessageEventAuthorized("dev-sweep", "EVT-NEW"); !ok || !authorized {
		t.Fatal("the new decision was not kept")
	}
}

func TestQueuedRevokeKeepsItsAuthorization(t *testing.T) {
	origEnabled := config.ChatwootEnabled
	origHard := config.ChatwootMessageDelete
	origForward := config.ChatwootForwardDeletes
	origIgnore := config.ChatwootIgnoreJids
	origClient := getChatwootClientFn
	origDM := deviceManager
	t.Cleanup(func() {
		config.ChatwootEnabled = origEnabled
		config.ChatwootMessageDelete = origHard
		config.ChatwootForwardDeletes = origForward
		config.ChatwootIgnoreJids = origIgnore
		getChatwootClientFn = origClient
		deviceManager = origDM
		ForgetMessageAuthorizationsForTest("dev-retry")
	})
	config.ChatwootEnabled = true
	config.ChatwootMessageDelete = true
	config.ChatwootForwardDeletes = true
	config.ChatwootIgnoreJids = nil
	deviceManager = NewDeviceManager(nil, nil, nil)
	deviceManager.AddDevice(NewDeviceInstance("dev-retry", nil, nil))

	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method)
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
	chatJID := "628123@s.whatsapp.net"
	repo := &authzLinkRepo{link: &chatstorage.ChatwootMessageLink{
		DeviceID: "dev-retry", WhatsAppMessageID: "m1", WhatsAppChatJID: chatJID,
		ChatwootMessageID: 8, ChatwootConversationID: 9, ChatwootConfigID: 3, ChatwootAccountID: 1,
	}}

	// The author's revoke was authorized and its row deleted, then Chatwoot failed.
	NoteMessageEventAuthorization("dev-retry", "REV-1", true)
	NoteMessageRevokeAuthorization("dev-retry", chatJID, "m1", true)
	payload := map[string]any{"event": "message.revoked", "payload": map[string]any{
		"id": "REV-1", "revoked_message_id": "m1", "chat_id": chatJID, "from": chatJID, "is_from_me": false,
	}}
	queue := &chatwootForwardQueueTestRepo{}
	if !enqueueChatwootForwardRetry(queue, "dev-retry", "message.revoked", payload, &chatwoot.HTTPStatusError{
		StatusCode: http.StatusInternalServerError, Op: "delete message",
	}) {
		t.Fatal("transient failure was not queued")
	}
	if _, stamped := payload[queuedAuthorizationKey]; stamped {
		t.Fatal("the decision was written into the live payload")
	}

	// A restart drops the in-memory decisions. The replay restores the queued one.
	ForgetMessageAuthorizationsForTest("dev-retry")
	if err := processChatwootForwardRetryEvent(repo, queue.events[0]); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(requests) != 1 || requests[0] != http.MethodDelete {
		t.Fatalf("authorized queued revoke did not hard-delete: %v", requests)
	}

	// A queued revoke without a decision is denied: no delete and no tombstone.
	requests = nil
	ForgetMessageAuthorizationsForTest("dev-retry")
	if err := processChatwootForwardRetryEvent(repo, &chatstorage.ChatwootForwardEvent{
		DeviceID:    "dev-retry",
		EventName:   "message.revoked",
		PayloadJSON: `{"payload":{"id":"REV-2","revoked_message_id":"m1","chat_id":"628123@s.whatsapp.net","from":"628123@s.whatsapp.net"}}`,
	}); err != nil {
		t.Fatalf("replay without a decision: %v", err)
	}
	if len(requests) != 0 {
		t.Fatalf("queued revoke without a decision reached Chatwoot: %v", requests)
	}

	// This account's own revoke without a recorded decision is queued as
	// authorized, as the live path treats it.
	own := map[string]any{"event": "message.revoked", "payload": map[string]any{
		"id": "REV-3", "revoked_message_id": "m1", "chat_id": chatJID, "from": chatJID, "is_from_me": true,
	}}
	if !enqueueChatwootForwardRetry(queue, "dev-retry", "message.revoked", own, &chatwoot.HTTPStatusError{
		StatusCode: http.StatusInternalServerError, Op: "delete message",
	}) {
		t.Fatal("own revoke was not queued")
	}
	if queued := queue.events[len(queue.events)-1].PayloadJSON; !strings.Contains(queued, `"chatwoot_authorized":true`) {
		t.Fatalf("own revoke queued without authorization: %s", queued)
	}
}

func TestChatwootSkipsChatExcludedOnlyByItsStoredLink(t *testing.T) {
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
	// The link was stored under the excluded LID. The events name only the phone.
	phone := "1555121200@s.whatsapp.net"
	repo := &authzLinkRepo{link: &chatstorage.ChatwootMessageLink{
		DeviceID: "dev", WhatsAppMessageID: "m1", WhatsAppChatJID: "111@lid",
		ChatwootMessageID: 8, ChatwootConversationID: 9, ChatwootConfigID: 3, ChatwootAccountID: 1,
		ChatwootContactInboxSourceID: "src",
	}}

	for _, tc := range []struct {
		event string
		data  map[string]any
	}{
		{"message.deleted", map[string]any{"deleted_message_id": "m1", "chat_id": phone, "from": phone}},
		{"message.ack", map[string]any{"receipt_type": "read", "ids": []string{"m1"}, "chat_id": phone, "from": phone}},
	} {
		if chatwootPayloadExcluded(tc.data) {
			t.Fatalf("%s payload must not be excluded by itself", tc.event)
		}
		if err := syncPayloadToChatwoot(context.Background(), map[string]any{"payload": tc.data}, tc.event, "dev", repo); err != nil {
			t.Fatalf("%s: %v", tc.event, err)
		}
		if hit {
			t.Fatalf("%s reached Chatwoot for a chat excluded by its stored link", tc.event)
		}
	}
}

func TestPayloadExclusionWildcardsApplyToDeliveredAddress(t *testing.T) {
	origChatwoot, origWebhook := config.ChatwootIgnoreJids, config.WhatsappWebhookIgnoreJids
	t.Cleanup(func() { config.ChatwootIgnoreJids, config.WhatsappWebhookIgnoreJids = origChatwoot, origWebhook })

	resolved := map[string]any{"chat_id": "628123@s.whatsapp.net", "from": "628123@s.whatsapp.net", "chat_lid": "111@lid", "from_lid": "111@lid"}
	config.ChatwootIgnoreJids = []string{"@lid"}
	if chatwootPayloadExcluded(resolved) {
		t.Error("@lid excluded a chat that resolved to a phone number")
	}
	if !chatwootPayloadExcluded(map[string]any{"chat_id": "111@lid", "from": "111@lid"}) {
		t.Error("@lid did not exclude a chat still addressed by its LID")
	}
	config.ChatwootIgnoreJids = []string{"111@lid"}
	if !chatwootPayloadExcluded(resolved) {
		t.Error("an exact LID entry did not exclude the resolved chat")
	}

	config.WhatsappWebhookIgnoreJids = []string{"628555@s.whatsapp.net"}
	receipt := map[string]any{"payload": map[string]any{"chat_id": "222@lid", "from": "222@lid", "sender_alt": "628555@s.whatsapp.net"}}
	if !shouldIgnoreWebhookJID(receipt) {
		t.Error("the webhook ignore list did not match sender_alt")
	}
}
