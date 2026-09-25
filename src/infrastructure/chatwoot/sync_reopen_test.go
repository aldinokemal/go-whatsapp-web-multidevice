package chatwoot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow"
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

	svc.restMediaPrePass(context.Background(), msg.DeviceID, chat, []*domainChatStorage.Message{msg}, nil, DefaultSyncOptions(), false)

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

// chatwootReopenStub stands in for a Chatwoot that already knows the contact
// and holds one *resolved* conversation for it, so a history sync gets all the
// way to posting. Message creation answers postStatus (200 with an id, or an
// error code); every request path is recorded in order, which is what lets a
// test assert not just whether toggle_status was called but when.
func chatwootReopenStub(t *testing.T, repo *chatwootSyncChatRepo, chatJID string, postStatus int) (*SyncService, func() []string) {
	t.Helper()
	return chatwootReopenStubWithToggleFailures(t, repo, chatJID, postStatus, 0)
}

// chatwootReopenStubWithToggleFailures is chatwootReopenStub whose
// toggle_status endpoint answers 500 to its first failFirst calls, so a test
// can drive the retry paths without touching the retry policy.
func chatwootReopenStubWithToggleFailures(t *testing.T, repo *chatwootSyncChatRepo, chatJID string, postStatus, failFirst int) (*SyncService, func() []string) {
	t.Helper()
	const contactID, conversationID = 7, 42
	var toggles int
	var mu sync.Mutex
	var events []string
	record := func(r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, r.Method+" "+r.URL.Path)
	}
	phone := utils.NormalizePhoneE164(chatJID)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record(r)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/contacts/search"):
			_ = json.NewEncoder(w).Encode(map[string]any{"payload": []map[string]any{{"id": contactID, "name": "Contact", "phone_number": phone}}})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, fmt.Sprintf("/contacts/%d/conversations", contactID)):
			_ = json.NewEncoder(w).Encode(map[string]any{"payload": []map[string]any{{"id": conversationID, "inbox_id": 2, "status": "resolved"}}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, fmt.Sprintf("/conversations/%d/messages", conversationID)):
			w.WriteHeader(postStatus)
			if postStatus == http.StatusOK {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 900})
			}
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, fmt.Sprintf("/conversations/%d/toggle_status", conversationID)):
			mu.Lock()
			toggles++
			failing := toggles <= failFirst
			mu.Unlock()
			if failing {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
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

// A resolved thread is reopened exactly once, and only after the first message
// has actually landed: the toggle must follow the first successful POST, not
// precede it. Reopening at resolution time was the REST-side twin of the
// pgimport defect -- any pending message that then failed to post reopened the
// thread with nothing added.
func TestSyncChatReopensOnlyAfterFirstPostedMessage(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	first := chatwootSyncChatMessage("wa-first")
	second := chatwootSyncChatMessage("wa-second")
	second.Timestamp = first.Timestamp.Add(time.Minute)
	repo := newChatwootSyncChatRepo(first, second)
	svc, events := chatwootReopenStub(t, repo, first.ChatJID, http.StatusOK)
	chat := &domainChatStorage.Chat{JID: first.ChatJID, Name: "Contact"}

	if err := svc.syncChat(context.Background(), first.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(first.DeviceID)); err != nil {
		t.Fatalf("syncChat: %v", err)
	}
	ev := events()
	if got := countEvents(ev, "/toggle_status"); got != 1 {
		t.Fatalf("toggle_status called %d times, want exactly 1 for two posted messages\n%v", got, ev)
	}
	if countEvents(ev, "/messages") != 2 {
		t.Fatalf("expected both messages to post\n%v", ev)
	}
	if post, toggle := indexOfEvent(ev, "/messages"), indexOfEvent(ev, "/toggle_status"); toggle < post {
		t.Fatalf("toggle_status (index %d) came before the first message POST (index %d); the reopen must follow a confirmed write\n%v", toggle, post, ev)
	}
}

// Every pending message fails to post -- Chatwoot rejects it, the media is
// gone, whatever the cause. Nothing was added, so the resolved thread must stay
// resolved. Before this change the reopen happened at resolution time and such
// a row reopened the thread on every restart until it aged out of the window.
func TestSyncChatDoesNotReopenWhenEveryPostFails(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	msg := chatwootSyncChatMessage("wa-rejected")
	repo := newChatwootReopenQueueRepo(msg)
	svc, events := chatwootReopenStub(t, repo.chatwootSyncChatRepo, msg.ChatJID, http.StatusUnprocessableEntity)
	svc.chatStorageRepo = repo
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	progress := NewSyncProgress(msg.DeviceID)
	if err := svc.syncChat(context.Background(), msg.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), progress); err != nil {
		t.Fatalf("syncChat: %v (per-message failures are counted, not returned)", err)
	}
	ev := events()
	if countEvents(ev, "/messages") == 0 {
		t.Fatalf("expected the post to be attempted\n%v", ev)
	}
	if got := countEvents(ev, "/toggle_status"); got != 0 {
		t.Fatalf("toggle_status called %d times, want 0 when nothing was posted\n%v", got, ev)
	}
	if progress.FailedMessages != 1 {
		t.Fatalf("FailedMessages = %d, want 1", progress.FailedMessages)
	}

	// The pre-arm wrote a live intent before posting; since nothing posted, it
	// must have been voided rather than left live to reopen an untouched
	// thread on the worker's next pass.
	queued := repo.only(t)
	if intent := decodeReopenIntent(t, queued); intent.EnqueuedAt != 0 {
		t.Fatalf("EnqueuedAt = %d, want 0 (voided): a live intent here would reopen the thread with nothing added", intent.EnqueuedAt)
	}
}

// The REST media pre-pass on the Postgres path holds the same invariant. Media
// that cannot be fetched never posts, so it must not reopen the thread -- on
// this run or any later one, since such a row never becomes linked.
func TestRESTMediaPrePassDoesNotReopenWhenNoAttachmentPosts(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	prevREST := config.ChatwootImportMediaWithREST
	defer func() {
		config.ChatwootReopenConversation = prevReopen
		config.ChatwootImportMediaWithREST = prevREST
	}()
	config.ChatwootReopenConversation = true
	config.ChatwootImportMediaWithREST = true

	msg := chatwootSyncChatMediaMessage("wa-expired-media")
	repo := newChatwootReopenQueueRepo(msg)
	svc, events := chatwootReopenStub(t, repo.chatwootSyncChatRepo, msg.ChatJID, http.StatusOK)
	svc.chatStorageRepo = repo
	chat := &domainChatStorage.Chat{JID: msg.ChatJID, Name: "Contact"}

	// No WhatsApp client: the download fails, the attachment is required, so
	// nothing is posted.
	svc.restMediaPrePass(context.Background(), msg.DeviceID, chat, []*domainChatStorage.Message{msg}, nil, DefaultSyncOptions(), false)

	ev := events()
	if countEvents(ev, "/conversations") == 0 {
		t.Fatalf("expected the pre-pass to resolve the conversation for pending media\n%v", ev)
	}
	if got := countEvents(ev, "/messages"); got != 0 {
		t.Fatalf("expected no message POST without an attachment, got %d\n%v", got, ev)
	}
	if got := countEvents(ev, "/toggle_status"); got != 0 {
		t.Fatalf("toggle_status called %d times, want 0 when no attachment posted\n%v", got, ev)
	}

	queued := repo.only(t)
	if intent := decodeReopenIntent(t, queued); intent.EnqueuedAt != 0 {
		t.Fatalf("EnqueuedAt = %d, want 0 (voided): a live intent here would reopen the thread with nothing added", intent.EnqueuedAt)
	}
}

// A failed reopen must stay retryable for as long as the pass keeps posting.
// Once a posted message's link is stored, no later pass will look at it
// again, so this pass is the only chance to bring the thread back. Here the
// toggle fails three times -- the whole retry budget of the first post -- and
// the second post repairs it on its first attempt.
func TestSyncChatRetriesReopenOnLaterPostsAfterToggleFailure(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	defer func() { config.ChatwootReopenConversation = prevReopen }()
	config.ChatwootReopenConversation = true

	first := chatwootSyncChatMessage("wa-first")
	second := chatwootSyncChatMessage("wa-second")
	second.Timestamp = first.Timestamp.Add(time.Minute)
	repo := newChatwootSyncChatRepo(first, second)
	svc, events := chatwootReopenStubWithToggleFailures(t, repo, first.ChatJID, http.StatusOK, 3)
	chat := &domainChatStorage.Chat{JID: first.ChatJID, Name: "Contact"}

	if err := svc.syncChat(context.Background(), first.DeviceID, chat, time.Time{}, nil, DefaultSyncOptions(), NewSyncProgress(first.DeviceID)); err != nil {
		t.Fatalf("syncChat: %v", err)
	}
	ev := events()
	if got := countEvents(ev, "/messages"); got != 2 {
		t.Fatalf("messages posted = %d, want 2; a failed toggle must not stop the pass\n%v", got, ev)
	}
	if got := countEvents(ev, "/toggle_status"); got != 4 {
		t.Fatalf("toggle_status called %d times, want 4: three failures on the first post, then success on the second\n%v", got, ev)
	}
	// The successful toggle is the last event, after the second POST.
	if last := ev[len(ev)-1]; !strings.HasSuffix(last, "/toggle_status") {
		t.Fatalf("last event = %q, want the repairing toggle_status after the second post\n%v", last, ev)
	}
}

// fakeMediaDownloader stands in for the WhatsApp download: it writes a small
// temp file and returns its path, which syncMessageWithOptions removes after
// posting, so every call has to produce a fresh one.
func fakeMediaDownloader(t *testing.T) func(context.Context, *domainChatStorage.Message, *whatsmeow.Client) (string, error) {
	t.Helper()
	return func(_ context.Context, msg *domainChatStorage.Message, _ *whatsmeow.Client) (string, error) {
		f, err := os.CreateTemp(t.TempDir(), "media-*.jpg")
		if err != nil {
			return "", err
		}
		if _, err := f.WriteString("jpeg bytes for " + msg.ID); err != nil {
			f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		return f.Name(), nil
	}
}

// The media pre-pass holds the same retry contract as syncChat: a toggle that
// fails on the first attachment is retried on the next one, because once these
// rows are linked (and pgimport then skips them) nothing downstream will
// reopen the thread. Three failures exhaust the first post's budget; the
// second post repairs it.
func TestRESTMediaPrePassRetriesReopenOnLaterPostsAfterToggleFailure(t *testing.T) {
	prevReopen := config.ChatwootReopenConversation
	prevREST := config.ChatwootImportMediaWithREST
	defer func() {
		config.ChatwootReopenConversation = prevReopen
		config.ChatwootImportMediaWithREST = prevREST
	}()
	config.ChatwootReopenConversation = true
	config.ChatwootImportMediaWithREST = true

	first := chatwootSyncChatMediaMessage("wa-media-first")
	second := chatwootSyncChatMediaMessage("wa-media-second")
	second.Timestamp = first.Timestamp.Add(time.Minute)
	repo := newChatwootSyncChatRepo(first, second)
	svc, events := chatwootReopenStubWithToggleFailures(t, repo, first.ChatJID, http.StatusOK, 3)
	svc.mediaDownloader = fakeMediaDownloader(t)
	chat := &domainChatStorage.Chat{JID: first.ChatJID, Name: "Contact"}

	svc.restMediaPrePass(context.Background(), first.DeviceID, chat, []*domainChatStorage.Message{first, second}, nil, DefaultSyncOptions(), false)

	ev := events()
	if got := countEvents(ev, "/messages"); got != 2 {
		t.Fatalf("attachments posted = %d, want 2\n%v", got, ev)
	}
	if got := countEvents(ev, "/toggle_status"); got != 4 {
		t.Fatalf("toggle_status called %d times, want 4: three failures on the first post, then success on the second\n%v", got, ev)
	}
	if last := ev[len(ev)-1]; !strings.HasSuffix(last, "/toggle_status") {
		t.Fatalf("last event = %q, want the repairing toggle_status after the second post\n%v", last, ev)
	}
}
