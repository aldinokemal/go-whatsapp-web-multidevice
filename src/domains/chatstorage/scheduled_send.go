package chatstorage

import "time"

// ScheduledSend is the durable worker record. PayloadJSON contains the
// normalized send request; AssetsJSON contains managed upload metadata.
type ScheduledSend struct {
	ID              string
	DeviceID        string
	MessageType     string
	PayloadJSON     string
	AssetsJSON      string
	Phone           string
	Summary         string
	ScheduledAt     time.Time
	NextRunAt       time.Time
	Timezone        string
	Recurrence      string
	WeekdaysJSON    string
	DayOfMonth      int
	EndAt           *time.Time
	OccurrenceLimit int
	OccurrenceCount int
	Attempts        int
	Status          string
	LeaseToken      string
	LeaseUntil      *time.Time
	LastRunAt       *time.Time
	LastMessageID   string
	// PendingMessageID is the WhatsApp message id of the occurrence being
	// attempted. It is chosen before the first attempt and reused by every
	// retry of that occurrence, so a send WhatsApp accepted but whose
	// acknowledgment was lost is not delivered a second time. It is empty
	// between occurrences.
	PendingMessageID string
	LastError        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
