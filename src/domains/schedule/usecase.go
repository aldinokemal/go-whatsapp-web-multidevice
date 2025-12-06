package schedule

import (
	"context"
	"time"
)

// IScheduleUsecase exposes management operations for scheduled messages.
type IScheduleUsecase interface {
	List(ctx context.Context, request ListScheduledMessagesRequest) ([]*ScheduledMessage, error)
	Get(ctx context.Context, id int64) (*ScheduledMessage, error)
	Create(ctx context.Context, payload ScheduleMessagePayload) (*ScheduledMessage, error)
	Update(ctx context.Context, id int64, payload ScheduleMessagePayload) (*ScheduledMessage, error)
	Delete(ctx context.Context, id int64) error
	RunNow(ctx context.Context, id int64) (*ScheduledMessage, error)
}

// ListScheduledMessagesRequest represents query parameters for listing scheduled messages.
type ListScheduledMessagesRequest struct {
	Statuses []Status `json:"statuses"`
	Limit    int      `json:"limit"`
	Offset   int      `json:"offset"`
}

// ScheduleMessagePayload mirrors the editable fields of a scheduled message.
type ScheduleMessagePayload struct {
	Phone          string     `json:"phone"`
	Message        string     `json:"message"`
	ReplyMessageID *string    `json:"reply_message_id"`
	IsForwarded    bool       `json:"is_forwarded"`
	Duration       *int       `json:"duration"`
	ScheduleAt     *time.Time `json:"schedule_at"`
}
