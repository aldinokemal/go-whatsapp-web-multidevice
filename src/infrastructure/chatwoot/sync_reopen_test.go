package chatwoot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
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

// chatwootSyncChatService wires a SyncService whose every REST call fails, and
// counts them. The failures are deliberate: each test here asserts on whether a
// request was made at all, so the stub never has to model a real response.
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

	if err := svc.restMediaPrePass(context.Background(), chat, []*domainChatStorage.Message{msg}, nil, DefaultSyncOptions(), false); err != nil {
		t.Fatalf("restMediaPrePass: %v", err)
	}

	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0 for a chat with no media", got)
	}
}

// chatwootFlakyLinkRepo fails the first link lookup and serves the real link on
// every call after it, reproducing a transient chatstorage read.
type chatwootFlakyLinkRepo struct {
	*chatwootSyncChatRepo
	lookups atomic.Int32
}

func (r *chatwootFlakyLinkRepo) GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID string) (*domainChatStorage.ChatwootMessageLink, error) {
	if r.lookups.Add(1) == 1 {
		return nil, errors.New("chatstorage temporarily unavailable")
	}
	return r.chatwootSyncChatRepo.GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID)
}

// A failed link lookup must not be read as "this message is pending". If it
// were, syncChat would resolve a conversation -- reopening a resolved thread --
// and syncMessageWithOptions would then repeat the lookup, find the link and
// post nothing: the thread reopened with nothing added, which is the exact
// regression this prefilter exists to prevent. The lookup failure has to abort
// the chat before any Chatwoot call.
func TestSyncChatAbortsWhenLinkLookupFails(t *testing.T) {
	msg := chatwootSyncChatMessage("wa-lookup-flaky")
	inner := newChatwootSyncChatRepo(msg)
	seedChatwootLink(t, inner, msg, 999)
	repo := &chatwootFlakyLinkRepo{chatwootSyncChatRepo: inner}

	svc, requests := chatwootSyncChatService(t, repo.chatwootSyncChatRepo)
	svc.chatStorageRepo = repo

	progress := NewSyncProgress(msg.DeviceID)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	err := svc.syncChat(context.Background(), msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), progress)
	if err == nil {
		t.Fatal("syncChat: expected the link-lookup failure to abort the chat")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0; a failed lookup must not reach FindOrCreateConversation", got)
	}
	// The second lookup would have reported the message as already linked, so
	// nothing was ever pending: proceeding could only have reopened the thread.
	if got := repo.lookups.Load(); got != 1 {
		t.Fatalf("link lookups = %d, want 1; the partition must stop at the first failure", got)
	}
}

// chatwootFailingIDLinkRepo fails the lookup for one specific message and
// serves the real link for every other, so a test can place a confirmed link
// before the failure.
type chatwootFailingIDLinkRepo struct {
	*chatwootSyncChatRepo
	failFor string
}

func (r *chatwootFailingIDLinkRepo) GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID string) (*domainChatStorage.ChatwootMessageLink, error) {
	if waMessageID == r.failFor {
		return nil, errors.New("chatstorage temporarily unavailable")
	}
	return r.chatwootSyncChatRepo.GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID)
}

// Aborting the chat must not throw away the progress already established. A
// message confirmed linked before the failing lookup is still in Chatwoot --
// the failure says nothing about it -- so it stays counted as synced even
// though the chat as a whole is abandoned.
func TestSyncChatKeepsLinkedProgressWhenALaterLookupFails(t *testing.T) {
	linked := chatwootSyncChatMessage("wa-linked-first")
	broken := chatwootSyncChatMessage("wa-lookup-breaks")
	inner := newChatwootSyncChatRepo(linked, broken)
	seedChatwootLink(t, inner, linked, 555)
	repo := &chatwootFailingIDLinkRepo{chatwootSyncChatRepo: inner, failFor: broken.ID}

	svc, requests := chatwootSyncChatService(t, repo.chatwootSyncChatRepo)
	svc.chatStorageRepo = repo

	progress := NewSyncProgress(linked.DeviceID)
	chat := &domainChatStorage.Chat{JID: linked.ChatJID, Name: "Contact"}

	if err := svc.syncChat(context.Background(), linked.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), progress); err == nil {
		t.Fatal("syncChat: expected the link-lookup failure to abort the chat")
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0; a failed lookup must not reach FindOrCreateConversation", got)
	}
	if progress.SyncedMessages != 1 {
		t.Fatalf("SyncedMessages = %d, want 1; the message confirmed linked before the failure is still in Chatwoot", progress.SyncedMessages)
	}
	if progress.FailedMessages != 1 {
		t.Fatalf("FailedMessages = %d, want 1; the message left unclassified by the abort must not vanish from the totals", progress.FailedMessages)
	}
}

// The pgimport media pre-pass resolves a conversation through REST. Media that
// is already linked must not send it there, or every auto-sync reopens the
// thread on the Postgres path too.
func TestRESTMediaPrePassSkipsAlreadyLinkedMedia(t *testing.T) {
	prev := config.ChatwootImportMediaWithREST
	defer func() { config.ChatwootImportMediaWithREST = prev }()
	config.ChatwootImportMediaWithREST = true

	msg := chatwootSyncChatMediaMessage("wa-media-linked")
	repo := newChatwootSyncChatRepo(msg)
	seedChatwootLink(t, repo, msg, 888)

	svc, requests := chatwootSyncChatService(t, repo)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	if err := svc.restMediaPrePass(context.Background(), chat, []*domainChatStorage.Message{msg}, nil, DefaultSyncOptions(), false); err != nil {
		t.Fatalf("restMediaPrePass: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Chatwoot received %d requests, want 0 when the media is already linked", got)
	}
}

// chatwootResolvedConversationStub stands in for a Chatwoot that knows the
// contact and holds one resolved conversation for it. toggle_status answers
// toggleStatus; every request is recorded in order.
func chatwootResolvedConversationStub(t *testing.T, repo *chatwootSyncChatRepo, chatJID string, toggleStatus int) (*SyncService, func() []string) {
	t.Helper()
	const contactID, conversationID = 7, 42
	var mu sync.Mutex
	var events []string
	phone := utils.NormalizePhoneE164(chatJID)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		events = append(events, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/contacts/search"):
			_ = json.NewEncoder(w).Encode(map[string]any{"payload": []map[string]any{{"id": contactID, "name": "Contact", "phone_number": phone}}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, fmt.Sprintf("/contacts/%d/conversations", contactID)):
			_ = json.NewEncoder(w).Encode(map[string]any{"payload": []map[string]any{{"id": conversationID, "inbox_id": 2, "status": "resolved"}}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, fmt.Sprintf("/conversations/%d/messages", conversationID)):
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 900})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, fmt.Sprintf("/conversations/%d/toggle_status", conversationID)):
			w.WriteHeader(toggleStatus)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	svc := NewSyncService(&Client{
		BaseURL:    server.URL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: server.Client(),
	}, repo)
	return svc, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}
}

func countEvents(events []string, suffix string) int {
	n := 0
	for _, e := range events {
		if strings.HasSuffix(e, suffix) {
			n++
		}
	}
	return n
}

func indexOfEvent(events []string, suffix string) int {
	for i, e := range events {
		if strings.HasSuffix(e, suffix) {
			return i
		}
	}
	return -1
}

// A genuinely new message still reopens the resolved thread, before it is
// posted, so the agent sees it.
func TestSyncChatReopensResolvedConversationBeforePosting(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	msg := chatwootSyncChatMessage("wa-new")
	repo := newChatwootSyncChatRepo(msg)
	svc, events := chatwootResolvedConversationStub(t, repo, msg.ChatJID, http.StatusOK)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	if err := svc.syncChat(context.Background(), msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(msg.DeviceID)); err != nil {
		t.Fatalf("syncChat: %v", err)
	}
	ev := events()
	if got := countEvents(ev, "/messages"); got != 1 {
		t.Fatalf("messages posted = %d, want 1\n%v", got, ev)
	}
	if toggle, post := indexOfEvent(ev, "/toggle_status"), indexOfEvent(ev, "/messages"); toggle < 0 || toggle > post {
		t.Fatalf("toggle_status at %d, message POST at %d; want the reopen before the post\n%v", toggle, post, ev)
	}
}

// FindOrCreateConversation only logs a failed reopen. If the sync posted
// anyway, the message would sit in a resolved thread, and once linked no later
// sync would reopen it. The chat must fail before posting so the next sync
// retries.
func TestSyncChatFailsWithoutPostingWhenReopenFails(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	msg := chatwootSyncChatMessage("wa-new")
	repo := newChatwootSyncChatRepo(msg)
	svc, events := chatwootResolvedConversationStub(t, repo, msg.ChatJID, http.StatusInternalServerError)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	if err := svc.syncChat(context.Background(), msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(msg.DeviceID)); err == nil {
		t.Fatal("syncChat: expected an error when the conversation stays resolved")
	}
	if ev := events(); countEvents(ev, "/messages") != 0 {
		t.Fatalf("a message was posted into the still-resolved conversation\n%v", ev)
	}
}
