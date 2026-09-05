package chatwoot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// chatwootReopenQueueRepo records what syncChat puts on the Chatwoot forward
// queue, deduping on the queue's real unique key (device, event, message id) so
// a test sees the same single row a repeated enqueue would collapse into.
type chatwootReopenQueueRepo struct {
	*chatwootSyncChatRepo
	queued   map[string]*domainChatStorage.ChatwootForwardEvent
	enqueues int
}

func newChatwootReopenQueueRepo(messages ...*domainChatStorage.Message) *chatwootReopenQueueRepo {
	return &chatwootReopenQueueRepo{
		chatwootSyncChatRepo: newChatwootSyncChatRepo(messages...),
		queued:               make(map[string]*domainChatStorage.ChatwootForwardEvent),
	}
}

func (r *chatwootReopenQueueRepo) EnqueueChatwootForwardEvent(event *domainChatStorage.ChatwootForwardEvent) error {
	r.enqueues++
	cloned := *event
	r.queued[event.DeviceID+"\x00"+event.EventName+"\x00"+event.WhatsAppMessageID] = &cloned
	return nil
}

func (r *chatwootReopenQueueRepo) only(t *testing.T) *domainChatStorage.ChatwootForwardEvent {
	t.Helper()
	if len(r.queued) != 1 {
		t.Fatalf("queued rows = %d, want exactly 1", len(r.queued))
	}
	for _, event := range r.queued {
		return event
	}
	return nil
}

func decodeReopenIntent(t *testing.T, event *domainChatStorage.ChatwootForwardEvent) ReopenIntent {
	t.Helper()
	var intent ReopenIntent
	if err := json.Unmarshal([]byte(event.PayloadJSON), &intent); err != nil {
		t.Fatalf("decode reopen intent: %v", err)
	}
	return intent
}

// The gap the in-pass retry cannot close: the message posts, its link is
// stored, and every toggle in the pass still fails. The next sync filters that
// message out as already linked, so nothing would ever revisit the thread. The
// reopen therefore has to leave the process on the retry queue -- as a
// conversation, not as a message, so the retry can never repost anything.
func TestSyncChatQueuesReopenIntentWhenEveryToggleFails(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	msg := chatwootSyncChatMessage("wa-posted-not-reopened")
	before := time.Now().Unix()
	repo := newChatwootReopenQueueRepo(msg)
	svc, events := chatwootReopenStubWithToggleFailures(t, repo.chatwootSyncChatRepo, msg.ChatJID, http.StatusOK, 99)
	svc.chatStorageRepo = repo
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	if err := svc.syncChat(context.Background(), msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(msg.DeviceID)); err != nil {
		t.Fatalf("syncChat: %v", err)
	}

	ev := events()
	if got := countEvents(ev, "/messages"); got != 1 {
		t.Fatalf("messages posted = %d, want 1\n%v", got, ev)
	}
	if got := countEvents(ev, "/toggle_status"); got == 0 {
		t.Fatalf("expected the in-pass reopen to be attempted\n%v", ev)
	}

	queued := repo.only(t)
	if queued.EventName != ReopenForwardEventName {
		t.Fatalf("EventName = %q, want %q", queued.EventName, ReopenForwardEventName)
	}
	if queued.DeviceID != msg.DeviceID {
		t.Fatalf("DeviceID = %q, want %q", queued.DeviceID, msg.DeviceID)
	}
	if want := ReopenIntentQueueKey(42); queued.WhatsAppMessageID != want {
		t.Fatalf("queue key = %q, want %q", queued.WhatsAppMessageID, want)
	}
	if queued.NextAttemptAt.IsZero() {
		t.Fatal("NextAttemptAt is zero; the row would be replayed with no backoff")
	}
	// The queued row must name the failure, not a placeholder: it is what an
	// operator reads out of the queue when a thread stays stuck.
	if !strings.Contains(queued.LastError, "toggle conversation status") {
		t.Fatalf("LastError = %q, want the toggle failure", queued.LastError)
	}

	intent := decodeReopenIntent(t, queued)
	if intent.ConversationID != 42 {
		t.Fatalf("ConversationID = %d, want 42", intent.ConversationID)
	}
	if intent.AccountID != 1 {
		t.Fatalf("intent account = %d, want 1", intent.AccountID)
	}
	if intent.TargetStatus != "open" {
		t.Fatalf("TargetStatus = %q, want %q", intent.TargetStatus, "open")
	}
	if intent.ChatJID != msg.ChatJID {
		t.Fatalf("ChatJID = %q, want %q", intent.ChatJID, msg.ChatJID)
	}
	// EnqueuedAt is what lets the worker tell this resolve from a later one.
	if intent.EnqueuedAt < before || intent.EnqueuedAt > time.Now().Unix() {
		t.Fatalf("EnqueuedAt = %d, want a stamp from this run (>= %d)", intent.EnqueuedAt, before)
	}

	// The message is linked, which is exactly why the reopen had to be
	// persisted: no later sync will post it again.
	link, err := repo.GetChatwootMessageLinkByWhatsAppID(msg.DeviceID, msg.ID)
	if err != nil {
		t.Fatalf("lookup link: %v", err)
	}
	if link == nil || link.ChatwootMessageID == 0 {
		t.Fatal("expected the posted message to be linked")
	}
}

// Several posted messages in one chat are one stuck thread, not several. The
// queue key is the conversation, so repeated failures collapse into a single
// row and the worker toggles once.
func TestSyncChatQueuesOneReopenIntentPerConversation(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	first := chatwootSyncChatMessage("wa-first")
	second := chatwootSyncChatMessage("wa-second")
	second.Timestamp = first.Timestamp.Add(time.Minute)
	repo := newChatwootReopenQueueRepo(first, second)
	svc, events := chatwootReopenStubWithToggleFailures(t, repo.chatwootSyncChatRepo, first.ChatJID, http.StatusOK, 99)
	svc.chatStorageRepo = repo
	chat := &domainChatStorage.Chat{JID: first.ChatJID, Name: "Contact"}

	if err := svc.syncChat(context.Background(), first.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(first.DeviceID)); err != nil {
		t.Fatalf("syncChat: %v", err)
	}
	if got := countEvents(events(), "/messages"); got != 2 {
		t.Fatalf("messages posted = %d, want 2", got)
	}
	repo.only(t)
}

// A reopen that succeeds inside the pass leaves nothing behind: the queue is
// for the failure case only.
func TestSyncChatQueuesNoReopenIntentWhenToggleSucceeds(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	msg := chatwootSyncChatMessage("wa-reopened")
	repo := newChatwootReopenQueueRepo(msg)
	svc, events := chatwootReopenStub(t, repo.chatwootSyncChatRepo, msg.ChatJID, http.StatusOK)
	svc.chatStorageRepo = repo
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	if err := svc.syncChat(context.Background(), msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(msg.DeviceID)); err != nil {
		t.Fatalf("syncChat: %v", err)
	}
	if got := countEvents(events(), "/toggle_status"); got != 1 {
		t.Fatalf("toggle_status called %d times, want 1", got)
	}
	if repo.enqueues != 0 {
		t.Fatalf("enqueued %d retry rows, want 0 after a successful reopen", repo.enqueues)
	}
}

// The REST media pre-pass on the Postgres path posts and links exactly like
// syncChat, and pgimport then skips those rows, so its failed reopen needs the
// same durable retry.
func TestRESTMediaPrePassQueuesReopenIntentWhenEveryToggleFails(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	prevREST := config.ChatwootImportMediaWithREST
	defer func() {
		config.ChatwootReopenConversation = prevReopen
		config.ChatwootImportMediaWithREST = prevREST
	}()
	config.ChatwootReopenConversation = true
	config.ChatwootImportMediaWithREST = true

	msg := chatwootSyncChatMediaMessage("wa-media-not-reopened")
	repo := newChatwootReopenQueueRepo(msg)
	svc, events := chatwootReopenStubWithToggleFailures(t, repo.chatwootSyncChatRepo, msg.ChatJID, http.StatusOK, 99)
	svc.chatStorageRepo = repo
	svc.mediaDownloader = fakeMediaDownloader(t)
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	svc.restMediaPrePass(context.Background(), msg.DeviceID, chat, []*domainChatStorage.Message{msg}, nil, DefaultSyncOptions(), false)

	if got := countEvents(events(), "/messages"); got != 1 {
		t.Fatalf("attachments posted = %d, want 1", got)
	}
	queued := repo.only(t)
	if queued.DeviceID != msg.DeviceID {
		t.Fatalf("DeviceID = %q, want the device the sync was running for", queued.DeviceID)
	}
	if intent := decodeReopenIntent(t, queued); intent.ConversationID != 42 {
		t.Fatalf("ConversationID = %d, want 42", intent.ConversationID)
	}
}

// A later sync that posts into the same thread and fails to reopen again must
// refresh the intent, not extend a dead one: the queue's ON CONFLICT rewrites
// payload_json, so the new EnqueuedAt restarts the window the worker allows
// itself. Without that, one exhausted row would swallow every later failure.
func TestReenqueuedReopenIntentRefreshesEnqueuedAt(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	msg := chatwootSyncChatMessage("wa-first-pass")
	repo := newChatwootReopenQueueRepo(msg)
	svc, _ := chatwootReopenStubWithToggleFailures(t, repo.chatwootSyncChatRepo, msg.ChatJID, http.StatusOK, 99)
	svc.chatStorageRepo = repo
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}
	conversation := &Conversation{ID: 42, Status: "resolved"}

	svc.persistReopenIntent(msg.DeviceID, conversation, chat.JID, errors.New("first failure"))
	first := decodeReopenIntent(t, repo.only(t))

	// Re-stamp the stored row as if it had been queued a day ago, then let a
	// later pass fail again.
	stale := first
	stale.EnqueuedAt = time.Now().Add(-24 * time.Hour).Unix()
	stalePayload, err := json.Marshal(stale)
	if err != nil {
		t.Fatalf("marshal stale intent: %v", err)
	}
	repo.only(t).PayloadJSON = string(stalePayload)

	svc.persistReopenIntent(msg.DeviceID, conversation, chat.JID, errors.New("second failure"))

	queued := repo.only(t)
	if repo.enqueues != 2 {
		t.Fatalf("enqueues = %d, want 2 collapsing into one row", repo.enqueues)
	}
	refreshed := decodeReopenIntent(t, queued)
	if refreshed.EnqueuedAt <= stale.EnqueuedAt {
		t.Fatalf("EnqueuedAt = %d, want it refreshed past the stale stamp %d", refreshed.EnqueuedAt, stale.EnqueuedAt)
	}
	if queued.LastError != "second failure" {
		t.Fatalf("LastError = %q, want the newer failure", queued.LastError)
	}
}
