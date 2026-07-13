package chatwoot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
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
