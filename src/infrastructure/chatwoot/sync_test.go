package chatwoot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
)

// TestTriggerAutoSync_NoopWhenGated verifies the connect-path entrypoint is a
// safe no-op under the documented gates and with a not-logged-in (nil) client.
// The call site fires on every WhatsApp connect event, so these guards must
// never panic or start work when they shouldn't.
func TestTriggerAutoSync_NoopWhenGated(t *testing.T) {
	prevEnabled := config.ChatwootEnabled
	prevImport := config.ChatwootImportMessages
	defer func() {
		config.ChatwootEnabled = prevEnabled
		config.ChatwootImportMessages = prevImport
	}()

	// Disabled integration: no-op regardless of the import flag.
	config.ChatwootEnabled = false
	config.ChatwootImportMessages = true
	TriggerAutoSync(nil, nil)

	// Enabled but import off: no-op.
	config.ChatwootEnabled = true
	config.ChatwootImportMessages = false
	TriggerAutoSync(nil, nil)

	// Enabled + import on, but no logged-in client (nil) means no storage JID
	// is available yet — must return without panicking or launching a sync.
	config.ChatwootImportMessages = true
	TriggerAutoSync(nil, nil)
}

func TestSyncProgress_SetRunning(t *testing.T) {
	p := NewSyncProgress("test-device")

	if p.Status != "idle" {
		t.Errorf("expected initial status 'idle', got '%s'", p.Status)
	}

	p.SetRunning()

	if p.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", p.Status)
	}

	if p.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
}

func TestSyncProgress_SetCompleted(t *testing.T) {
	p := NewSyncProgress("test-device")
	p.SetRunning()
	p.SetCompleted()

	if p.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", p.Status)
	}

	if p.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestSyncProgress_SetFailed(t *testing.T) {
	p := NewSyncProgress("test-device")
	p.SetRunning()

	testErr := &testError{msg: "test error"}
	p.SetFailed(testErr)

	if p.Status != "failed" {
		t.Errorf("expected status 'failed', got '%s'", p.Status)
	}

	if p.Error != "test error" {
		t.Errorf("expected error 'test error', got '%s'", p.Error)
	}
}

func TestSyncProgress_Counters(t *testing.T) {
	p := NewSyncProgress("test-device")

	p.SetTotals(10, 100)
	if p.TotalChats != 10 || p.TotalMessages != 100 {
		t.Errorf("expected totals (10, 100), got (%d, %d)", p.TotalChats, p.TotalMessages)
	}

	p.IncrementSyncedChats()
	p.IncrementSyncedChats()
	if p.SyncedChats != 2 {
		t.Errorf("expected synced chats 2, got %d", p.SyncedChats)
	}

	p.IncrementSyncedMessages()
	p.IncrementSyncedMessages()
	p.IncrementSyncedMessages()
	if p.SyncedMessages != 3 {
		t.Errorf("expected synced messages 3, got %d", p.SyncedMessages)
	}

	p.IncrementFailedMessages()
	if p.FailedMessages != 1 {
		t.Errorf("expected failed messages 1, got %d", p.FailedMessages)
	}

	p.AddMessages(50)
	if p.TotalMessages != 150 {
		t.Errorf("expected total messages 150, got %d", p.TotalMessages)
	}
}

func TestSyncProgress_Clone(t *testing.T) {
	p := NewSyncProgress("test-device")
	p.SetRunning()
	p.SetTotals(10, 100)
	p.IncrementSyncedChats()
	p.UpdateChat("test-chat")

	cloned := p.Clone()

	if cloned.DeviceID != p.DeviceID {
		t.Error("clone DeviceID mismatch")
	}
	if cloned.Status != p.Status {
		t.Error("clone Status mismatch")
	}
	if cloned.TotalChats != p.TotalChats {
		t.Error("clone TotalChats mismatch")
	}
	if cloned.CurrentChat != p.CurrentChat {
		t.Error("clone CurrentChat mismatch")
	}
}

func TestSyncProgress_IsRunning(t *testing.T) {
	p := NewSyncProgress("test-device")

	if p.IsRunning() {
		t.Error("expected IsRunning false initially")
	}

	p.SetRunning()
	if !p.IsRunning() {
		t.Error("expected IsRunning true after SetRunning")
	}

	p.SetCompleted()
	if p.IsRunning() {
		t.Error("expected IsRunning false after SetCompleted")
	}
}

func TestDefaultSyncOptions(t *testing.T) {
	opts := DefaultSyncOptions()

	if opts.DaysLimit != 3 {
		t.Errorf("expected DaysLimit 3, got %d", opts.DaysLimit)
	}

	if !opts.IncludeMedia {
		t.Error("expected IncludeMedia true")
	}

	if !opts.IncludeGroups {
		t.Error("expected IncludeGroups true")
	}

	if opts.MaxMessagesPerChat != 500 {
		t.Errorf("expected MaxMessagesPerChat 500, got %d", opts.MaxMessagesPerChat)
	}

	if opts.BatchSize != 10 {
		t.Errorf("expected BatchSize 10, got %d", opts.BatchSize)
	}

	if opts.DelayBetweenBatches != 500*time.Millisecond {
		t.Errorf("expected DelayBetweenBatches 500ms, got %v", opts.DelayBetweenBatches)
	}
}

func TestGetExtensionForMediaType(t *testing.T) {
	tests := []struct {
		mediaType string
		filename  string
		expected  string
	}{
		{"image", "", ".jpg"},
		{"video", "", ".mp4"},
		{"audio", "", ".oga"},
		{"ptt", "", ".oga"},
		{"document", "", ".bin"},
		{"sticker", "", ".webp"},
		{"unknown", "", ""},
		{"image", "photo.png", ".png"},
		{"document", "report.pdf", ".pdf"},
	}

	for _, tt := range tests {
		result := getExtensionForMediaType(tt.mediaType, tt.filename)
		if result != tt.expected {
			t.Errorf("getExtensionForMediaType(%s, %s) = %s, expected %s",
				tt.mediaType, tt.filename, result, tt.expected)
		}
	}
}

// testError is a simple error implementation for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{"nil error", nil, false},
		{"4xx", &HTTPStatusError{StatusCode: 400}, false},
		{"401", &HTTPStatusError{StatusCode: 401}, false},
		{"422", &HTTPStatusError{StatusCode: 422}, false},
		{"429", &HTTPStatusError{StatusCode: 429}, true},
		{"500", &HTTPStatusError{StatusCode: 500}, true},
		{"502", &HTTPStatusError{StatusCode: 502}, true},
		{"503", &HTTPStatusError{StatusCode: 503}, true},
		{"non-HTTP error", &testError{msg: "network"}, true},
		{"registry unavailable", ErrClientRegistryUnavailable, false},
		{"wrapped registry unavailable", fmt.Errorf("resolve: %w", ErrClientRegistryUnavailable), false},
	}
	for _, tt := range tests {
		if got := Retryable(tt.err); got != tt.retryable {
			t.Errorf("%s: Retryable(%v) = %v, want %v", tt.name, tt.err, got, tt.retryable)
		}
	}
}

func TestRetrySyncOp_DoesNotRetry4xx(t *testing.T) {
	// 4xx is a validation/auth error — hitting it 3 times would waste
	// backoff time and spam Chatwoot.
	attempts := 0
	err := retrySyncOp(context.Background(), 3, func() error {
		attempts++
		return &HTTPStatusError{StatusCode: 400, Op: "test", Body: "bad"}
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt on 4xx, got %d", attempts)
	}
}

func TestRetrySyncOp_Retries5xxUntilSuccess(t *testing.T) {
	attempts := 0
	err := retrySyncOp(context.Background(), 3, func() error {
		attempts++
		if attempts < 2 {
			return &HTTPStatusError{StatusCode: 503, Op: "test", Body: "unavailable"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetrySyncOp_RetriesUnknownErrors(t *testing.T) {
	// Non-HTTP errors (network, timeout) should retry.
	attempts := 0
	_ = retrySyncOp(context.Background(), 3, func() error {
		attempts++
		return fmt.Errorf("connection refused")
	})
	if attempts != 3 {
		t.Errorf("expected 3 attempts on generic error, got %d", attempts)
	}
}

func TestHTTPStatusError_Message(t *testing.T) {
	err := &HTTPStatusError{StatusCode: 429, Op: "create message", Body: "rate limited"}
	got := err.Error()
	want := "chatwoot create message: status 429 body rate limited"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// syncChatsRepo serves a fixed chat list to SyncHistory.
type syncChatsRepo struct {
	domainChatStorage.IChatStorageRepository
	chats []*domainChatStorage.Chat
}

func (r syncChatsRepo) GetChats(*domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	return r.chats, nil
}

// syncTestLIDs is a WhatsApp LID store holding phone JID -> LID mappings.
type syncTestLIDs struct {
	store.LIDStore
	pnToLID map[types.JID]types.JID
}

func (s syncTestLIDs) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	return s.pnToLID[pn], nil
}

func (s syncTestLIDs) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	for pn, mapped := range s.pnToLID {
		if mapped == lid {
			return pn, nil
		}
	}
	return types.JID{}, nil
}

// TestSyncHistorySkipsChatIgnoredByOtherAddress: CHATWOOT_IGNORE_JIDS may name
// a chat by its LID while history stores the phone JID, or the reverse.
// Either form keeps the chat out of the import.
func TestSyncHistorySkipsChatIgnoredByOtherAddress(t *testing.T) {
	prevIgnore := config.ChatwootIgnoreJids
	t.Cleanup(func() { config.ChatwootIgnoreJids = prevIgnore })

	pn := types.NewJID("628111111111", types.DefaultUserServer)
	lid := types.NewJID("111111111111111", types.HiddenUserServer)
	waClient := &whatsmeow.Client{Store: &store.Device{LIDs: syncTestLIDs{pnToLID: map[types.JID]types.JID{pn: lid}}}}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	for _, tt := range []struct{ stored, ignored types.JID }{{pn, lid}, {lid, pn}} {
		config.ChatwootIgnoreJids = []string{tt.ignored.String()}
		svc := NewSyncService(&Client{BaseURL: server.URL, APIToken: "t", AccountID: 1, InboxID: 2, HTTPClient: server.Client()},
			syncChatsRepo{chats: []*domainChatStorage.Chat{{JID: tt.stored.String()}}})
		progress, err := svc.SyncHistory(context.Background(), "device-a@s.whatsapp.net", waClient, SyncOptions{DaysLimit: 1})
		if err != nil {
			t.Fatalf("SyncHistory: %v", err)
		}
		if got := requests.Load(); got != 0 || progress.SyncedChats != 0 || progress.FailedChats != 0 {
			t.Errorf("chat %s ignored as %s: Chatwoot got %d requests, synced/failed %d/%d; want it skipped",
				tt.stored, tt.ignored, got, progress.SyncedChats, progress.FailedChats)
		}
	}
}

func TestSyncHistoryWildcardIgnoresOnlyStoredAddress(t *testing.T) {
	prevIgnore := config.ChatwootIgnoreJids
	t.Cleanup(func() { config.ChatwootIgnoreJids = prevIgnore })
	config.ChatwootIgnoreJids = []string{"@lid"}

	pn := types.NewJID("628111111111", types.DefaultUserServer)
	lid := types.NewJID("111111111111111", types.HiddenUserServer)
	waClient := &whatsmeow.Client{Store: &store.Device{LIDs: syncTestLIDs{pnToLID: map[types.JID]types.JID{pn: lid}}}}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := NewSyncService(&Client{BaseURL: server.URL, APIToken: "t", AccountID: 1, InboxID: 2, HTTPClient: server.Client()},
		syncChatsRepo{chats: []*domainChatStorage.Chat{{JID: pn.String()}}})
	if _, err := svc.SyncHistory(context.Background(), "device-a@s.whatsapp.net", waClient, SyncOptions{DaysLimit: 1}); err != nil {
		t.Fatalf("SyncHistory: %v", err)
	}
	if requests.Load() == 0 {
		t.Error("@lid skipped a phone-stored chat because of its LID alias")
	}
}
