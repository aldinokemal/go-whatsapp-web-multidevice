package send

import (
	"context"
	"strings"
	"time"
)

// ScheduleOptions controls delayed and recurring message delivery. Empty
// ScheduledAt preserves the existing immediate-send behavior.
type ScheduleOptions struct {
	ScheduledAt     string `json:"scheduled_at,omitempty" form:"scheduled_at"`
	Timezone        string `json:"timezone,omitempty" form:"timezone"`
	Recurrence      string `json:"recurrence,omitempty" form:"recurrence"`
	Weekdays        []int  `json:"weekdays,omitempty" form:"weekdays"`
	DayOfMonth      int    `json:"day_of_month,omitempty" form:"day_of_month"`
	EndAt           string `json:"end_at,omitempty" form:"end_at"`
	OccurrenceLimit int    `json:"occurrence_limit,omitempty" form:"occurrence_limit"`
}

func (s ScheduleOptions) IsScheduled() bool {
	return strings.TrimSpace(s.ScheduledAt) != ""
}

// Schedule represents a device-scoped scheduled send returned by the API.
type Schedule struct {
	ID              string     `json:"id"`
	MessageType     string     `json:"message_type"`
	Phone           string     `json:"phone"`
	Summary         string     `json:"summary,omitempty"`
	Status          string     `json:"status"`
	ScheduledAt     time.Time  `json:"scheduled_at"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	Timezone        string     `json:"timezone"`
	Recurrence      string     `json:"recurrence"`
	Weekdays        []int      `json:"weekdays,omitempty"`
	DayOfMonth      int        `json:"day_of_month,omitempty"`
	EndAt           *time.Time `json:"end_at,omitempty"`
	OccurrenceLimit int        `json:"occurrence_limit,omitempty"`
	OccurrenceCount int        `json:"occurrence_count"`
	Attempts        int        `json:"attempts"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	LastMessageID   string     `json:"last_message_id,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type ScheduleFilter struct {
	Status      string `json:"status,omitempty"`
	Search      string `json:"search,omitempty"`
	MessageType string `json:"message_type,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
}

// PaginationResponse mirrors the chat list envelope so both list endpoints
// page the same way.
type PaginationResponse struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

type ScheduleListResponse struct {
	Data       []Schedule         `json:"data"`
	Pagination PaginationResponse `json:"pagination"`
}

type IScheduleUsecase interface {
	List(ctx context.Context, filter ScheduleFilter) (ScheduleListResponse, error)
	Get(ctx context.Context, id string) (*Schedule, error)
	Pause(ctx context.Context, id string) error
	Resume(ctx context.Context, id string) error
	Cancel(ctx context.Context, id string) error
}
