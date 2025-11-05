package schedule

import (
	"context"
	"database/sql"
	"strings"
	"time"

	domainSchedule "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/schedule"
)

// SQLiteRepository persists scheduled messages using SQLite.
type SQLiteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository constructs a scheduled message repository backed by SQLite.
func NewSQLiteRepository(db *sql.DB) domainSchedule.IScheduledMessageRepository {
	return &SQLiteRepository{db: db}
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanScheduledMessage(scanner rowScanner) (*domainSchedule.ScheduledMessage, error) {
	var (
		replyID sql.NullString
		dur     sql.NullInt64
		errText sql.NullString
		msgID   sql.NullString
		sentAt  sql.NullTime
		m       domainSchedule.ScheduledMessage
	)

	if err := scanner.Scan(
		&m.ID,
		&m.Phone,
		&m.Message,
		&replyID,
		&m.IsForwarded,
		&dur,
		&m.ScheduleAt,
		&m.Status,
		&m.Attempts,
		&errText,
		&msgID,
		&m.CreatedAt,
		&m.UpdatedAt,
		&sentAt,
	); err != nil {
		return nil, err
	}

	if replyID.Valid {
		reply := replyID.String
		m.ReplyMessageID = &reply
	}
	if dur.Valid {
		duration := int(dur.Int64)
		m.Duration = &duration
	}
	if errText.Valid {
		errStr := errText.String
		m.Error = &errStr
	}
	if msgID.Valid {
		msgStr := msgID.String
		m.MessageID = &msgStr
	}
	if sentAt.Valid {
		t := sentAt.Time.UTC()
		m.SentAt = &t
	}

	m.ScheduleAt = m.ScheduleAt.UTC()
	m.CreatedAt = m.CreatedAt.UTC()
	m.UpdatedAt = m.UpdatedAt.UTC()

	return &m, nil
}

// Create stores a scheduled message and returns its identifier.
func (r *SQLiteRepository) Create(ctx context.Context, message *domainSchedule.ScheduledMessage) (int64, error) {
	now := time.Now().UTC()
	message.CreatedAt = now
	message.UpdatedAt = now
	message.Status = domainSchedule.StatusPending
	message.Attempts = 0
	message.ScheduleAt = message.ScheduleAt.UTC()

	var (
		replyID sql.NullString
		errText sql.NullString
		msgID   sql.NullString
		sentAt  sql.NullTime
		dur     sql.NullInt64
	)

	if message.ReplyMessageID != nil && *message.ReplyMessageID != "" {
		replyID = sql.NullString{String: *message.ReplyMessageID, Valid: true}
	}
	if message.Duration != nil {
		dur = sql.NullInt64{Int64: int64(*message.Duration), Valid: true}
	}

	result, err := r.db.ExecContext(
		ctx,
		`INSERT INTO scheduled_messages (
			phone, message, reply_message_id, is_forwarded, duration,
			schedule_at, status, attempts, error, message_id, created_at, updated_at, sent_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.Phone,
		message.Message,
		replyID,
		message.IsForwarded,
		dur,
		message.ScheduleAt.UTC(),
		message.Status,
		message.Attempts,
		errText,
		msgID,
		message.CreatedAt,
		message.UpdatedAt,
		sentAt,
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	message.ID = id
	return id, nil
}

// FetchPending retrieves up to limit scheduled messages that are due.
func (r *SQLiteRepository) FetchPending(ctx context.Context, limit int) ([]*domainSchedule.ScheduledMessage, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := r.db.QueryContext(
		ctx,
		`SELECT 
			id, phone, message, reply_message_id, is_forwarded, duration,
			schedule_at, status, attempts, error, message_id, created_at, updated_at, sent_at
		FROM scheduled_messages
		WHERE status = ? AND schedule_at <= ?
		ORDER BY schedule_at ASC
		LIMIT ?`,
		domainSchedule.StatusPending,
		time.Now().UTC(),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*domainSchedule.ScheduledMessage
	for rows.Next() {
		message, err := scanScheduledMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}

	return messages, rows.Err()
}

// MarkProcessing updates a scheduled message to the sending state if it is still pending.
func (r *SQLiteRepository) MarkProcessing(ctx context.Context, id int64) (bool, error) {
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE scheduled_messages
		 SET status = ?, attempts = attempts + 1, updated_at = ?
		 WHERE id = ? AND status = ?`,
		domainSchedule.StatusSending,
		time.Now().UTC(),
		id,
		domainSchedule.StatusPending,
	)
	if err != nil {
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected == 1, nil
}

// MarkSent marks a scheduled message as successfully sent.
func (r *SQLiteRepository) MarkSent(ctx context.Context, id int64, messageID string, sentAt time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE scheduled_messages
		 SET status = ?, message_id = ?, sent_at = ?, updated_at = ?, error = NULL
		 WHERE id = ?`,
		domainSchedule.StatusSent,
		messageID,
		sentAt.UTC(),
		time.Now().UTC(),
		id,
	)
	return err
}

// MarkFailed marks a scheduled message as failed with the provided error message.
func (r *SQLiteRepository) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE scheduled_messages
		 SET status = ?, error = ?, updated_at = ?
		 WHERE id = ?`,
		domainSchedule.StatusFailed,
		errMsg,
		time.Now().UTC(),
		id,
	)
	return err
}

// List retrieves scheduled messages using optional status filters with pagination.
func (r *SQLiteRepository) List(ctx context.Context, filter domainSchedule.ListFilter) ([]*domainSchedule.ScheduledMessage, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	var queryBuilder strings.Builder
	queryBuilder.WriteString(`SELECT 
			id, phone, message, reply_message_id, is_forwarded, duration,
			schedule_at, status, attempts, error, message_id, created_at, updated_at, sent_at
		FROM scheduled_messages`)

	var args []any
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, status := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, status)
		}
		queryBuilder.WriteString(" WHERE status IN (" + strings.Join(placeholders, ",") + ")")
	}

	queryBuilder.WriteString(" ORDER BY schedule_at ASC LIMIT ? OFFSET ?")
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*domainSchedule.ScheduledMessage
	for rows.Next() {
		message, err := scanScheduledMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}

	return messages, rows.Err()
}

// GetByID retrieves a scheduled message by its identifier.
func (r *SQLiteRepository) GetByID(ctx context.Context, id int64) (*domainSchedule.ScheduledMessage, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT 
			id, phone, message, reply_message_id, is_forwarded, duration,
			schedule_at, status, attempts, error, message_id, created_at, updated_at, sent_at
		FROM scheduled_messages
		WHERE id = ?`,
		id,
	)

	return scanScheduledMessage(row)
}

// Update modifies the editable fields of a scheduled message if it is pending or failed.
func (r *SQLiteRepository) Update(ctx context.Context, id int64, update domainSchedule.ScheduledMessageUpdate) (*domainSchedule.ScheduledMessage, error) {
	now := time.Now().UTC()

	var replyID sql.NullString
	if update.ReplyMessageID != nil && *update.ReplyMessageID != "" {
		replyID = sql.NullString{String: *update.ReplyMessageID, Valid: true}
	}

	var dur sql.NullInt64
	if update.Duration != nil {
		dur = sql.NullInt64{Int64: int64(*update.Duration), Valid: true}
	}

	result, err := r.db.ExecContext(
		ctx,
		`UPDATE scheduled_messages
		 SET phone = ?, message = ?, reply_message_id = ?, is_forwarded = ?, duration = ?,
		     schedule_at = ?, status = ?, attempts = 0, error = NULL, message_id = NULL,
		     sent_at = NULL, updated_at = ?
		 WHERE id = ? AND status IN (?, ?)`,
		update.Phone,
		update.Message,
		replyID,
		update.IsForwarded,
		dur,
		update.ScheduleAt.UTC(),
		domainSchedule.StatusPending,
		now,
		id,
		domainSchedule.StatusPending,
		domainSchedule.StatusFailed,
	)
	if err != nil {
		return nil, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, sql.ErrNoRows
	}

	return r.GetByID(ctx, id)
}

// Delete removes a scheduled message if it has not been sent yet.
func (r *SQLiteRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(
		ctx,
		`DELETE FROM scheduled_messages
		 WHERE id = ? AND status IN (?, ?)`,
		id,
		domainSchedule.StatusPending,
		domainSchedule.StatusFailed,
	)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// ResetStuck transitions messages stuck in the sending state back to pending.
func (r *SQLiteRepository) ResetStuck(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	result, err := r.db.ExecContext(
		ctx,
		`UPDATE scheduled_messages
		 SET status = ?, updated_at = ?
		 WHERE status = ? AND updated_at <= ?`,
		domainSchedule.StatusPending,
		time.Now().UTC(),
		domainSchedule.StatusSending,
		cutoff,
	)
	if err != nil {
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	return rowsAffected, nil
}
