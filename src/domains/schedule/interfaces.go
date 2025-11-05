package schedule

import (
	"context"
	"time"
)

// IScheduledMessageRepository defines storage operations for scheduled messages.
type IScheduledMessageRepository interface {
	Create(ctx context.Context, message *ScheduledMessage) (int64, error)
	FetchPending(ctx context.Context, limit int) ([]*ScheduledMessage, error)
	MarkProcessing(ctx context.Context, id int64) (bool, error)
	MarkSent(ctx context.Context, id int64, messageID string, sentAt time.Time) error
	MarkFailed(ctx context.Context, id int64, errMsg string) error
	ResetStuck(ctx context.Context, olderThan time.Duration) (int64, error)
	List(ctx context.Context, filter ListFilter) ([]*ScheduledMessage, error)
	GetByID(ctx context.Context, id int64) (*ScheduledMessage, error)
	Update(ctx context.Context, id int64, update ScheduledMessageUpdate) (*ScheduledMessage, error)
	Delete(ctx context.Context, id int64) error
}

// ListFilter defines filtering and pagination options for retrieving scheduled messages.
type ListFilter struct {
	Statuses []Status
	Limit    int
	Offset   int
}

// ScheduledMessageUpdate captures the editable fields for a scheduled message.
type ScheduledMessageUpdate struct {
	Phone          string
	Message        string
	ReplyMessageID *string
	IsForwarded    bool
	Duration       *int
	ScheduleAt     time.Time
}
