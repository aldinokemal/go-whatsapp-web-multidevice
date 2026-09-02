package chatwoot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// chatwootSyncChatRepo adds message reads to the link repo so syncChat can run
// against an in-memory chat.
type chatwootSyncChatRepo struct {
	*chatwootSyncLinkRepo
	messages []*domainChatStorage.Message
}

func (r *chatwootSyncChatRepo) GetMessages(_ *domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	return r.messages, nil
}

func newChatwootSyncChatRepo(messages ...*domainChatStorage.Message) *chatwootSyncChatRepo {
	return &chatwootSyncChatRepo{
		chatwootSyncLinkRepo: newChatwootSyncLinkTestRepo(),
		messages:             messages,
	}
}

func chatwootSyncChatMessage(id string) *domainChatStorage.Message {
	return &domainChatStorage.Message{
		ID:        id,
		DeviceID:  "device-a@s.whatsapp.net",
		ChatJID:   "628123456789@s.whatsapp.net",
		Content:   "hello",
		Timestamp: time.Date(2026, time.June, 6, 10, 0, 0, 0, time.UTC),
	}
}

func seedChatwootLink(t *testing.T, repo *chatwootSyncChatRepo, msg *domainChatStorage.Message, chatwootMessageID int) {
	t.Helper()
	if err := repo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
		DeviceID:          msg.DeviceID,
		WhatsAppMessageID: msg.ID,
		WhatsAppChatJID:   msg.ChatJID,
		ChatwootMessageID: chatwootMessageID,
		SourceID:          "WAID:" + msg.ID,
		Direction:         "incoming",
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
}

func chatwootSyncChatService(t *testing.T, repo *chatwootSyncChatRepo) (*SyncService, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	return NewSyncService(&Client{
		BaseURL:    server.URL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: server.Client(),
	}, repo), &requests
}

// A history sync over a chat whose messages are all already in Chatwoot must not
// resolve the conversation at all. FindOrCreateConversation reopens a resolved
// conversation, and auto-sync runs on every connect, so resolving here reopened
// every resolved conversation in the inbox on every restart.
func TestSyncChatLeavesConversationUntouchedWhenAllMessagesLinked(t *testing.T) {
	msg := chatwootSyncChatMessage("wa-already-linked")
	repo := newChatwootSyncChatRepo(msg)
	seedChatwootLink(t, repo, msg, 777)

	svc, requests := chatwootSyncChatService(t, repo)
	progress := NewSyncProgress(msg.DeviceID)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	if err := svc.syncChat(context.Background(), msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), progress); err != nil {
		t.Fatalf("syncChat: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0 when every message is already linked", got)
	}
	if progress.SyncedMessages != 1 {
		t.Fatalf("SyncedMessages = %d, want 1 (already-linked messages still count as synced)", progress.SyncedMessages)
	}
}

// A chat with no messages inside the time window must not resolve a conversation
// either — there is nothing to post into it.
func TestSyncChatLeavesConversationUntouchedWhenNoMessages(t *testing.T) {
	repo := newChatwootSyncChatRepo()
	svc, requests := chatwootSyncChatService(t, repo)
	progress := NewSyncProgress("device-a@s.whatsapp.net")
	chat := &domainChatStorage.Chat{JID: "628123456789@s.whatsapp.net", Name: "Contact"}

	if err := svc.syncChat(context.Background(), "device-a@s.whatsapp.net", chat, time.Time{}, nil, DefaultSyncOptions(), progress); err != nil {
		t.Fatalf("syncChat: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0 for a chat with no messages", got)
	}
}

// One unlinked message is enough to make the chat worth a conversation: the
// contact/conversation lookup must still happen so the message can be posted.
func TestSyncChatResolvesConversationWhenAMessageIsPending(t *testing.T) {
	linked := chatwootSyncChatMessage("wa-already-linked")
	pending := chatwootSyncChatMessage("wa-pending")
	repo := newChatwootSyncChatRepo(linked, pending)
	seedChatwootLink(t, repo, linked, 777)

	svc, requests := chatwootSyncChatService(t, repo)
	progress := NewSyncProgress(pending.DeviceID)
	chat := &domainChatStorage.Chat{JID: pending.ChatJID, Name: "Contact"}

	// The stub server answers 500, so the contact lookup fails and syncChat
	// returns an error. What matters is that it tried: the conversation is only
	// resolved when there is something new to post.
	if err := svc.syncChat(context.Background(), pending.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), progress); err == nil {
		t.Fatal("syncChat: expected an error from the failing contact lookup")
	}
	if got := requests.Load(); got == 0 {
		t.Fatal("Chatwoot received 0 requests, want the contact/conversation lookup for a pending message")
	}
}

func chatwootSyncChatMediaMessage(id string) *domainChatStorage.Message {
	msg := chatwootSyncChatMessage(id)
	msg.MediaType = "image"
	msg.URL = "https://mmg.whatsapp.net/file"
	msg.MediaKey = []byte("key")
	return msg
}

// The Postgres path carried the same defect as the REST one. Its media pre-pass
// resolved a conversation whenever the chat held any downloadable media, even
// when every one of those messages was already in Chatwoot -- which reopened the
// conversation for nothing on each auto-sync.
func TestRESTMediaPrePassLeavesConversationUntouchedWhenMediaAlreadyLinked(t *testing.T) {
	prev := config.ChatwootImportMediaWithREST
	defer func() { config.ChatwootImportMediaWithREST = prev }()
	config.ChatwootImportMediaWithREST = true

	msg := chatwootSyncChatMediaMessage("wa-media-linked")
	repo := newChatwootSyncChatRepo(msg)
	seedChatwootLink(t, repo, msg, 888)

	svc, requests := chatwootSyncChatService(t, repo)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	svc.restMediaPrePass(context.Background(), chat, []*domainChatStorage.Message{msg}, nil, DefaultSyncOptions(), false)

	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0 when the media is already linked", got)
	}
}

// Media that is not in Chatwoot yet still has to reach the pre-pass, so the
// attachment lands before pgimport writes the message.
func TestRESTMediaPrePassResolvesConversationForPendingMedia(t *testing.T) {
	prev := config.ChatwootImportMediaWithREST
	defer func() { config.ChatwootImportMediaWithREST = prev }()
	config.ChatwootImportMediaWithREST = true

	linked := chatwootSyncChatMediaMessage("wa-media-linked")
	pending := chatwootSyncChatMediaMessage("wa-media-pending")
	repo := newChatwootSyncChatRepo(linked, pending)
	seedChatwootLink(t, repo, linked, 888)

	svc, requests := chatwootSyncChatService(t, repo)
	chat := &domainChatStorage.Chat{JID: pending.ChatJID, Name: "Contact"}

	// The stub server answers 500, so the contact lookup fails and the pre-pass
	// gives up with a warning. What matters is that it tried.
	svc.restMediaPrePass(context.Background(), chat, []*domainChatStorage.Message{linked, pending}, nil, DefaultSyncOptions(), false)

	if got := requests.Load(); got == 0 {
		t.Fatal("Chatwoot received 0 requests, want the contact/conversation lookup for pending media")
	}
}

// A text-only chat has nothing for the pre-pass to do, so it must not resolve a
// conversation either.
func TestRESTMediaPrePassSkipsChatWithoutMedia(t *testing.T) {
	prev := config.ChatwootImportMediaWithREST
	defer func() { config.ChatwootImportMediaWithREST = prev }()
	config.ChatwootImportMediaWithREST = true

	msg := chatwootSyncChatMessage("wa-text-only")
	repo := newChatwootSyncChatRepo(msg)

	svc, requests := chatwootSyncChatService(t, repo)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	svc.restMediaPrePass(context.Background(), chat, []*domainChatStorage.Message{msg}, nil, DefaultSyncOptions(), false)

	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0 for a chat with no media", got)
	}
}
