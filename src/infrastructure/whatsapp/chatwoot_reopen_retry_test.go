package whatsapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot"
)

// chatwootReopenQueueRepo serves one due queue row and records how the worker
// finished with it.
type chatwootReopenQueueRepo struct {
	domainChatStorage.IChatStorageRepository
	due    []*domainChatStorage.ChatwootForwardEvent
	done   []int64
	failed []int64
}

func (r *chatwootReopenQueueRepo) ListDueChatwootForwardEvents(_ time.Time, _ int) ([]*domainChatStorage.ChatwootForwardEvent, error) {
	return r.due, nil
}

func (r *chatwootReopenQueueRepo) MarkChatwootForwardEventDone(id int64) error {
	r.done = append(r.done, id)
	return nil
}

func (r *chatwootReopenQueueRepo) MarkChatwootForwardEventFailed(id int64, _ string, _ time.Time) error {
	r.failed = append(r.failed, id)
	return nil
}

// chatwootConversationBody is what the stub server answers the conversation GET
// with. wrap puts it under a "payload" key, as some Chatwoot versions do.
type chatwootConversationBody struct {
	status         string
	lastActivityAt time.Time
	wrap           bool
	omitStatus     bool
}

func (b chatwootConversationBody) json() any {
	conv := map[string]any{"id": 42}
	if !b.omitStatus {
		conv["status"] = b.status
	}
	if !b.lastActivityAt.IsZero() {
		conv["last_activity_at"] = b.lastActivityAt.Unix()
		// Chatwoot renders updated_at as a float in some versions.
		conv["updated_at"] = float64(b.lastActivityAt.Unix()) + 0.5
	}
	if b.wrap {
		return map[string]any{"payload": conv}
	}
	return conv
}

// chatwootReopenServer answers the conversation GET with the given body and
// records every request path, so a test can assert the worker toggles when it
// should and never posts a message.
func chatwootReopenServer(t *testing.T, body chatwootConversationBody) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/toggle_status"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/conversations/42"):
			_ = json.NewEncoder(w).Encode(body.json())
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), paths...)
	}
}

func resolvedConversation(lastActivityAt time.Time) chatwootConversationBody {
	return chatwootConversationBody{status: "resolved", lastActivityAt: lastActivityAt}
}

func stubChatwootClientForReopen(t *testing.T, server *httptest.Server, accountID int) {
	t.Helper()
	orig := getChatwootClientFn
	t.Cleanup(func() { getChatwootClientFn = orig })
	getChatwootClientFn = func(string) (*chatwoot.ResolvedConfig, error) {
		return &chatwoot.ResolvedConfig{
			DeviceID: "device-a@s.whatsapp.net",
			Client: &chatwoot.Client{
				BaseURL:    server.URL,
				APIToken:   "token",
				AccountID:  accountID,
				InboxID:    2,
				HTTPClient: server.Client(),
			},
		}, nil
	}
}

func reopenQueueRow(t *testing.T, intent chatwoot.ReopenIntent) *domainChatStorage.ChatwootForwardEvent {
	t.Helper()
	if intent.EnqueuedAt == 0 {
		intent.EnqueuedAt = time.Now().Unix()
	}
	payload, err := json.Marshal(intent)
	if err != nil {
		t.Fatalf("marshal intent: %v", err)
	}
	return &domainChatStorage.ChatwootForwardEvent{
		ID:                7,
		DeviceID:          "device-a@s.whatsapp.net",
		EventName:         chatwoot.ReopenForwardEventName,
		WhatsAppMessageID: chatwoot.ReopenIntentQueueKey(intent.ConversationID),
		PayloadJSON:       string(payload),
	}
}

func stuckReopenIntent() chatwoot.ReopenIntent {
	return chatwoot.ReopenIntent{ConversationID: 42, AccountID: 1, TargetStatus: "open", ChatJID: "628123456789@s.whatsapp.net"}
}

func enableChatwootReopen(t *testing.T) {
	t.Helper()
	prev := config.ChatwootReopenConversation
	t.Cleanup(func() { config.ChatwootReopenConversation = prev })
	config.ChatwootReopenConversation = true
}

func countPaths(paths []string, suffix string) int {
	n := 0
	for _, p := range paths {
		if strings.HasSuffix(p, suffix) {
			n++
		}
	}
	return n
}

// The queued intent is replayed as a toggle and nothing else: the row carries a
// conversation, so the worker cannot repost the message that was already
// delivered and linked during the sync. A successful toggle drops the row.
func TestProcessDueChatwootForwardRetriesReopensQueuedConversation(t *testing.T) {
	enableChatwootReopen(t)
	server, paths := chatwootReopenServer(t, resolvedConversation(time.Now().Add(-time.Hour)))
	stubChatwootClientForReopen(t, server, 1)

	repo := &chatwootReopenQueueRepo{due: []*domainChatStorage.ChatwootForwardEvent{
		reopenQueueRow(t, stuckReopenIntent()),
	}}

	processDueChatwootForwardRetries(repo)

	got := paths()
	if n := countPaths(got, "/toggle_status"); n != 1 {
		t.Fatalf("toggle_status called %d times, want 1\n%v", n, got)
	}
	if n := countPaths(got, "/messages"); n != 0 {
		t.Fatalf("the retry posted %d messages, want 0; it must never repost\n%v", n, got)
	}
	if len(repo.done) != 1 || repo.done[0] != 7 {
		t.Fatalf("done = %v, want the intent to be cleared after a successful reopen", repo.done)
	}
	if len(repo.failed) != 0 {
		t.Fatalf("failed = %v, want none", repo.failed)
	}
}

// Some Chatwoot versions wrap the conversation in a "payload" object, as every
// other decoder in the client already tolerates.
func TestReplayChatwootReopenIntentReadsWrappedConversationPayload(t *testing.T) {
	enableChatwootReopen(t)
	body := resolvedConversation(time.Now().Add(-time.Hour))
	body.wrap = true
	server, paths := chatwootReopenServer(t, body)
	stubChatwootClientForReopen(t, server, 1)

	if err := replayChatwootReopenIntent(reopenQueueRow(t, stuckReopenIntent())); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if n := countPaths(paths(), "/toggle_status"); n != 1 {
		t.Fatalf("toggle_status called %d times, want 1 for a wrapped payload\n%v", n, paths())
	}
}

// A body with no status is unreadable, not "not resolved". Treating it as the
// latter would delete the row on a Chatwoot that answered oddly, so the replay
// has to fail and be rescheduled instead.
func TestReplayChatwootReopenIntentRetriesUnreadableConversation(t *testing.T) {
	enableChatwootReopen(t)
	body := resolvedConversation(time.Now().Add(-time.Hour))
	body.omitStatus = true
	server, paths := chatwootReopenServer(t, body)
	stubChatwootClientForReopen(t, server, 1)

	repo := &chatwootReopenQueueRepo{due: []*domainChatStorage.ChatwootForwardEvent{
		reopenQueueRow(t, stuckReopenIntent()),
	}}
	processDueChatwootForwardRetries(repo)

	if n := countPaths(paths(), "/toggle_status"); n != 0 {
		t.Fatalf("toggle_status called %d times, want 0 for an undecodable conversation", n)
	}
	if len(repo.failed) != 1 || len(repo.done) != 0 {
		t.Fatalf("failed=%v done=%v, want the row rescheduled rather than deleted", repo.failed, repo.done)
	}
}

// Replaying against a thread somebody already opened must be a no-op, not a
// second toggle: the intent is satisfied and the row goes away.
func TestProcessDueChatwootForwardRetriesSkipsAlreadyOpenConversation(t *testing.T) {
	enableChatwootReopen(t)
	server, paths := chatwootReopenServer(t, chatwootConversationBody{status: "open"})
	stubChatwootClientForReopen(t, server, 1)

	repo := &chatwootReopenQueueRepo{due: []*domainChatStorage.ChatwootForwardEvent{
		reopenQueueRow(t, stuckReopenIntent()),
	}}

	processDueChatwootForwardRetries(repo)

	if n := countPaths(paths(), "/toggle_status"); n != 0 {
		t.Fatalf("toggle_status called %d times for an already-open conversation\n%v", n, paths())
	}
	if len(repo.done) != 1 {
		t.Fatalf("done = %v, want the satisfied intent to be cleared", repo.done)
	}
}

// The case the status alone cannot see: the thread was reopened by an inbound
// message and an agent resolved it again. It is resolved, like when the intent
// was queued, but the resolve is a newer decision and reopening would undo an
// agent's action -- the regression #822 removes. Activity newer than the intent
// is what gives it away.
func TestReplayChatwootReopenIntentDropsResolveNewerThanTheIntent(t *testing.T) {
	enableChatwootReopen(t)
	server, paths := chatwootReopenServer(t, resolvedConversation(time.Now()))
	stubChatwootClientForReopen(t, server, 1)

	intent := stuckReopenIntent()
	intent.EnqueuedAt = time.Now().Add(-2 * time.Hour).Unix()

	repo := &chatwootReopenQueueRepo{due: []*domainChatStorage.ChatwootForwardEvent{reopenQueueRow(t, intent)}}
	processDueChatwootForwardRetries(repo)

	if n := countPaths(paths(), "/toggle_status"); n != 0 {
		t.Fatalf("toggle_status called %d times, want 0 for a resolve made after the intent\n%v", n, paths())
	}
	if len(repo.done) != 1 {
		t.Fatalf("done = %v, want the stale intent to be cleared", repo.done)
	}
}

// Activity from before the intent is the sync's own post, which is exactly what
// the intent was queued for -- it must not be read as a newer decision.
func TestReplayChatwootReopenIntentReopensWhenActivityPredatesTheIntent(t *testing.T) {
	enableChatwootReopen(t)
	server, paths := chatwootReopenServer(t, resolvedConversation(time.Now().Add(-2*time.Hour)))
	stubChatwootClientForReopen(t, server, 1)

	intent := stuckReopenIntent()
	intent.EnqueuedAt = time.Now().Add(-time.Hour).Unix()

	if err := replayChatwootReopenIntent(reopenQueueRow(t, intent)); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if n := countPaths(paths(), "/toggle_status"); n != 1 {
		t.Fatalf("toggle_status called %d times, want 1\n%v", n, paths())
	}
}

// A device rebound to another Chatwoot account must not have a stale
// conversation id toggled against it: ids are only unique within an account.
func TestReplayChatwootReopenIntentDropsAccountMismatch(t *testing.T) {
	enableChatwootReopen(t)
	server, paths := chatwootReopenServer(t, resolvedConversation(time.Now().Add(-time.Hour)))
	stubChatwootClientForReopen(t, server, 9)

	if err := replayChatwootReopenIntent(reopenQueueRow(t, stuckReopenIntent())); err != nil {
		t.Fatalf("replay: %v, want the mismatch to be dropped", err)
	}
	if got := paths(); len(got) != 0 {
		t.Fatalf("Chatwoot received %v, want no request for a mismatched account", got)
	}
}

// Reopening turned off between the sync and the retry means the intent is moot.
func TestReplayChatwootReopenIntentDropsWhenReopenDisabled(t *testing.T) {
	prev := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prev }()
	config.ChatwootReopenConversation = false

	server, paths := chatwootReopenServer(t, resolvedConversation(time.Now().Add(-time.Hour)))
	stubChatwootClientForReopen(t, server, 1)

	if err := replayChatwootReopenIntent(reopenQueueRow(t, stuckReopenIntent())); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got := paths(); len(got) != 0 {
		t.Fatalf("Chatwoot received %v, want no request while reopening is disabled", got)
	}
}

// The window bounds how long a reopen chases a thread from when it was queued.
// Past it the resolve is likelier deliberate than stale, so the row is dropped
// instead of eventually reopening a conversation an agent closed on purpose.
func TestReplayChatwootReopenIntentGivesUpOutsideTheWindow(t *testing.T) {
	enableChatwootReopen(t)
	server, paths := chatwootReopenServer(t, resolvedConversation(time.Time{}))
	stubChatwootClientForReopen(t, server, 1)

	intent := stuckReopenIntent()
	intent.EnqueuedAt = time.Now().Add(-maxChatwootReopenRetryWindow - time.Hour).Unix()

	if err := replayChatwootReopenIntent(reopenQueueRow(t, intent)); err != nil {
		t.Fatalf("replay: %v, want the expired intent to be dropped", err)
	}
	if got := paths(); len(got) != 0 {
		t.Fatalf("Chatwoot received %v, want no request once the window has closed", got)
	}
}

// A row with no EnqueuedAt cannot be reasoned about at all -- neither window
// nor staleness -- so it is dropped rather than replayed blind.
func TestReplayChatwootReopenIntentDropsMalformedRow(t *testing.T) {
	enableChatwootReopen(t)
	server, paths := chatwootReopenServer(t, resolvedConversation(time.Time{}))
	stubChatwootClientForReopen(t, server, 1)

	row := reopenQueueRow(t, stuckReopenIntent())
	row.PayloadJSON = `{"conversation_id":42,"account_id":1}`

	if err := replayChatwootReopenIntent(row); err != nil {
		t.Fatalf("replay: %v, want the malformed row to be dropped", err)
	}
	if got := paths(); len(got) != 0 {
		t.Fatalf("Chatwoot received %v, want no request for a row with no enqueue stamp", got)
	}
}

// prearmRefreshFailsRepo backs a real chatwoot.SyncService for the sync half
// of the maintainer's scenario, and then serves the exact same durable rows
// to processDueChatwootForwardRetries for the "later run" half, so one test
// proves the full chain with the real code on both sides -- not a
// reimplementation of either.
//
// EnqueueChatwootForwardEvent lets exactly the first call through (the
// pre-arm syncChat makes before posting) and fails every call after that
// (the refresh persistReopenIntent makes once the live toggle fails),
// reproducing "queue write fails" on the write that is not the pre-arm.
type prearmRefreshFailsRepo struct {
	domainChatStorage.IChatStorageRepository
	mu       sync.Mutex
	chats    []*domainChatStorage.Chat
	messages []*domainChatStorage.Message
	links    map[string]*domainChatStorage.ChatwootMessageLink
	queue    map[string]*domainChatStorage.ChatwootForwardEvent
	enqueues int
}

func newPrearmRefreshFailsRepo(chat *domainChatStorage.Chat, messages ...*domainChatStorage.Message) *prearmRefreshFailsRepo {
	return &prearmRefreshFailsRepo{
		chats:    []*domainChatStorage.Chat{chat},
		messages: messages,
		links:    make(map[string]*domainChatStorage.ChatwootMessageLink),
		queue:    make(map[string]*domainChatStorage.ChatwootForwardEvent),
	}
}

func (r *prearmRefreshFailsRepo) GetChats(*domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	return r.chats, nil
}

func (r *prearmRefreshFailsRepo) GetMessages(*domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	return r.messages, nil
}

func (r *prearmRefreshFailsRepo) GetChatwootMessageLinkByWhatsAppID(deviceID, waMessageID string) (*domainChatStorage.ChatwootMessageLink, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	link := r.links[deviceID+"\x00"+waMessageID]
	if link == nil {
		return nil, nil
	}
	cloned := *link
	return &cloned, nil
}

func (r *prearmRefreshFailsRepo) UpsertChatwootMessageLink(link *domainChatStorage.ChatwootMessageLink) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cloned := *link
	r.links[link.DeviceID+"\x00"+link.WhatsAppMessageID] = &cloned
	return nil
}

func (r *prearmRefreshFailsRepo) EnqueueChatwootForwardEvent(event *domainChatStorage.ChatwootForwardEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enqueues++
	if r.enqueues > 1 {
		return errors.New("database is locked")
	}
	cloned := *event
	cloned.ID = 1
	r.queue[event.DeviceID+"\x00"+event.EventName+"\x00"+event.WhatsAppMessageID] = &cloned
	return nil
}

func (r *prearmRefreshFailsRepo) ListDueChatwootForwardEvents(time.Time, int) ([]*domainChatStorage.ChatwootForwardEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := make([]*domainChatStorage.ChatwootForwardEvent, 0, len(r.queue))
	for _, event := range r.queue {
		cloned := *event
		events = append(events, &cloned)
	}
	return events, nil
}

func (r *prearmRefreshFailsRepo) MarkChatwootForwardEventDone(id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, event := range r.queue {
		if event.ID == id {
			delete(r.queue, key)
		}
	}
	return nil
}

func (r *prearmRefreshFailsRepo) MarkChatwootForwardEventFailed(id int64, lastError string, nextAttemptAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.queue {
		if event.ID == id {
			event.LastError = lastError
			event.NextAttemptAt = nextAttemptAt
			event.Attempts++
		}
	}
	return nil
}

// TestChatwootReopenIntentSurvivesFailedQueueRefresh runs the maintainer's
// named scenario end to end, through the real chatwoot.SyncService and the
// real forward-retry worker: post succeeds -> link stored -> the live toggle
// fails -> the post-failure queue write (persistReopenIntent's refresh) also
// fails -> a later run still repairs the conversation, without reposting
// anything. It is the pre-arm written before posting, not the refresh, that
// makes this possible: the refresh failing is a no-op because the pre-armed
// row is still sitting in the queue underneath it.
func TestChatwootReopenIntentSurvivesFailedQueueRefresh(t *testing.T) {
	enableChatwootReopen(t)

	const contactID, conversationID = 7, 42
	msg := &domainChatStorage.Message{
		ID:        "wa-refresh-fails",
		DeviceID:  "device-a@s.whatsapp.net",
		ChatJID:   "628123456789@s.whatsapp.net",
		Content:   "hello",
		Timestamp: time.Now(),
	}
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}
	repo := newPrearmRefreshFailsRepo(chat, msg)

	var toggles atomic.Int32
	var messagePosts atomic.Int32
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/contacts/search"):
			_ = json.NewEncoder(w).Encode(map[string]any{"payload": []map[string]any{{"id": contactID, "name": "Contact", "phone_number": "+628123456789"}}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/contacts/7/conversations"):
			_ = json.NewEncoder(w).Encode(map[string]any{"payload": []map[string]any{{"id": conversationID, "inbox_id": 2, "status": "resolved"}}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/conversations/42/messages"):
			messagePosts.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 900})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/conversations/42/toggle_status"):
			n := toggles.Add(1)
			// Fail every toggle during the sync pass (its retry budget is 3);
			// the later run's single attempt (the 4th call overall) succeeds.
			if n <= 3 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/conversations/42"):
			// Still resolved when the later run checks state before replaying.
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "resolved"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &chatwoot.Client{
		BaseURL:    server.URL,
		APIToken:   "token",
		AccountID:  1,
		InboxID:    2,
		HTTPClient: server.Client(),
	}

	// Sync half: real SyncService, real syncChat/persistReopenIntent.
	svc := chatwoot.NewSyncService(client, repo)
	if _, err := svc.SyncHistory(context.Background(), msg.DeviceID, nil, chatwoot.DefaultSyncOptions()); err != nil {
		t.Fatalf("SyncHistory: %v", err)
	}

	if got := messagePosts.Load(); got != 1 {
		t.Fatalf("messages posted during sync = %d, want 1", got)
	}
	if got := toggles.Load(); got != 3 {
		t.Fatalf("toggle_status calls during sync = %d, want 3 (the retry budget, all failing)", got)
	}
	repo.mu.Lock()
	queuedAfterSync := len(repo.queue)
	repo.mu.Unlock()
	if queuedAfterSync != 1 {
		t.Fatalf("queued rows after sync = %d, want 1 (the pre-arm; the refresh failed and must not have removed it)", queuedAfterSync)
	}

	// Later run: the real forward-retry worker, against the same durable row.
	stubChatwootClientForReopen(t, server, 1)
	processDueChatwootForwardRetries(repo)

	if got := toggles.Load(); got != 4 {
		t.Fatalf("toggle_status calls total = %d, want 4 (3 failing during sync, 1 succeeding on the later run)", got)
	}
	if got := messagePosts.Load(); got != 1 {
		t.Fatalf("messages posted total = %d, want still 1: the later run must never repost", got)
	}
	repo.mu.Lock()
	queuedAfterReplay := len(repo.queue)
	repo.mu.Unlock()
	if queuedAfterReplay != 0 {
		t.Fatalf("queued rows after the later run = %d, want 0 (cleared after the reopen finally succeeded)", queuedAfterReplay)
	}
}
