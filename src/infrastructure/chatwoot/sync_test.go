package chatwoot

import (
	"testing"
	"time"
)

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
		{"audio", "", ".ogg"},
		{"ptt", "", ".ogg"},
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
