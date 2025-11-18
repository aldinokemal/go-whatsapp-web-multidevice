package schedule

import "time"

// Status represents the lifecycle status of a scheduled message.
type Status string

const (
	StatusPending Status = "pending"
	StatusSending Status = "sending"
	StatusSent    Status = "sent"
	StatusFailed  Status = "failed"
)

// ScheduledMessage holds the persisted payload for a deferred WhatsApp message.
type ScheduledMessage struct {
	ID             int64      `db:"id" json:"id"`
	Phone          string     `db:"phone" json:"phone"`
	Message        string     `db:"message" json:"message"`
	ReplyMessageID *string    `db:"reply_message_id" json:"reply_message_id,omitempty"`
	IsForwarded    bool       `db:"is_forwarded" json:"is_forwarded"`
	Duration       *int       `db:"duration" json:"duration,omitempty"`
	ScheduleAt     time.Time  `db:"schedule_at" json:"schedule_at"`
	Status         Status     `db:"status" json:"status"`
	Attempts       int        `db:"attempts" json:"attempts"`
	Error          *string    `db:"error" json:"error,omitempty"`
	MessageID      *string    `db:"message_id" json:"message_id,omitempty"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updated_at"`
	SentAt         *time.Time `db:"sent_at" json:"sent_at,omitempty"`
}
