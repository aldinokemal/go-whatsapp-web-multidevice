package chatwoot

import (
	"sync"
	"time"
)

// SyncState tracks what has been synced to avoid duplicates
type SyncState struct {
	DeviceID        string    `db:"device_id"`
	ChatJID         string    `db:"chat_jid"`
	LastSyncedMsgID string    `db:"last_synced_msg_id"`
	LastSyncedTime  time.Time `db:"last_synced_time"`
	SyncStatus      string    `db:"sync_status"` // pending, in_progress, completed, failed
	MessagesSynced  int       `db:"messages_synced"`
	MessagesFailed  int       `db:"messages_failed"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

// SyncProgress tracks overall sync progress
type SyncProgress struct {
	DeviceID       string     `json:"device_id"`
	Status         string     `json:"status"` // idle, running, completed, failed
	TotalChats     int        `json:"total_chats"`
	SyncedChats    int        `json:"synced_chats"`
	FailedChats    int        `json:"failed_chats"`
	TotalMessages  int        `json:"total_messages"`
	SyncedMessages int        `json:"synced_messages"`
	FailedMessages int        `json:"failed_messages"`
	CurrentChat    string     `json:"current_chat,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	Error          string     `json:"error,omitempty"`
	mu             sync.RWMutex
}

// SyncOptions configures the sync behavior
type SyncOptions struct {
	DaysLimit           int           // Days of history to import
	IncludeMedia        bool          // Download and sync media attachments
	IncludeGroups       bool          // Include group chats
	MaxMessagesPerChat  int           // Limit messages per chat to prevent huge syncs
	BatchSize           int           // Messages per batch (for rate limiting)
	DelayBetweenBatches time.Duration // Delay between batches
}

// SyncRequest is the API request for triggering a sync
type SyncRequest struct {
	DeviceID     string `json:"device_id,omitempty"`
	DaysLimit    int    `json:"days_limit,omitempty"`
	IncludeMedia bool   `json:"include_media"`
	IncludeGroups bool  `json:"include_groups"`
}

// SyncResponse is the API response for sync operations
type SyncResponse struct {
	Status   string        `json:"status"`
	Message  string        `json:"message"`
	Progress *SyncProgress `json:"progress,omitempty"`
}

// DefaultSyncOptions returns reasonable default sync options
func DefaultSyncOptions() SyncOptions {
	return SyncOptions{
		DaysLimit:           3,
		IncludeMedia:        true,
		IncludeGroups:       true,
		MaxMessagesPerChat:  500,
		BatchSize:           10,
		DelayBetweenBatches: 500 * time.Millisecond,
	}
}

// NewSyncProgress creates a new sync progress tracker
func NewSyncProgress(deviceID string) *SyncProgress {
	return &SyncProgress{
		DeviceID: deviceID,
		Status:   "idle",
	}
}

// SetRunning marks the sync as running
func (p *SyncProgress) SetRunning() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Status = "running"
	now := time.Now()
	p.StartedAt = &now
}

// SetCompleted marks the sync as completed
func (p *SyncProgress) SetCompleted() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Status = "completed"
	now := time.Now()
	p.CompletedAt = &now
}

// SetFailed marks the sync as failed
func (p *SyncProgress) SetFailed(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Status = "failed"
	now := time.Now()
	p.CompletedAt = &now
	if err != nil {
		p.Error = err.Error()
	}
}

// UpdateChat updates the current chat being synced
func (p *SyncProgress) UpdateChat(chatJID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.CurrentChat = chatJID
}

// IncrementSyncedChats increments the synced chats counter
func (p *SyncProgress) IncrementSyncedChats() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SyncedChats++
}

// IncrementFailedChats increments the failed chats counter
func (p *SyncProgress) IncrementFailedChats() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FailedChats++
}

// IncrementSyncedMessages increments the synced messages counter
func (p *SyncProgress) IncrementSyncedMessages() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SyncedMessages++
}

// IncrementFailedMessages increments the failed messages counter
func (p *SyncProgress) IncrementFailedMessages() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FailedMessages++
}

// SetTotals sets the total counts
func (p *SyncProgress) SetTotals(chats, messages int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.TotalChats = chats
	p.TotalMessages = messages
}

// AddMessages adds to total messages count
func (p *SyncProgress) AddMessages(count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.TotalMessages += count
}

// Clone returns a thread-safe copy of the progress
func (p *SyncProgress) Clone() SyncProgress {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return SyncProgress{
		DeviceID:       p.DeviceID,
		Status:         p.Status,
		TotalChats:     p.TotalChats,
		SyncedChats:    p.SyncedChats,
		FailedChats:    p.FailedChats,
		TotalMessages:  p.TotalMessages,
		SyncedMessages: p.SyncedMessages,
		FailedMessages: p.FailedMessages,
		CurrentChat:    p.CurrentChat,
		StartedAt:      p.StartedAt,
		CompletedAt:    p.CompletedAt,
		Error:          p.Error,
	}
}

// IsRunning returns true if sync is currently running
func (p *SyncProgress) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Status == "running"
}
