package chatstorage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Repository defines the interface for chat storage operations
type Repository interface {
	// Chat operations
	StoreChat(chat *Chat) error
	GetChat(jid string) (*Chat, error)
	GetChats(filter *ChatFilter) ([]*Chat, error)
	UpdateChatLastMessage(jid string, lastMessageTime time.Time) error
	DeleteChat(jid string) error

	// Message operations
	StoreMessage(message *Message) error
	StoreMessagesBatch(messages []*Message) error
	GetMessage(id, chatJID string) (*Message, error)
	GetMessages(filter *MessageFilter) ([]*Message, error)
	GetMediaInfo(id, chatJID string) (*MediaInfo, error)
	UpdateMediaInfo(id, chatJID string, mediaInfo *MediaInfo) error
	DeleteMessage(id, chatJID string) error
	DeleteMessagesByChat(chatJID string) error

	// Statistics
	GetChatMessageCount(chatJID string) (int64, error)
	GetTotalMessageCount() (int64, error)
}

// SQLiteRepository implements Repository using SQLite
type SQLiteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository creates a new SQLite repository
func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

// StoreChat creates or updates a chat
func (r *SQLiteRepository) StoreChat(chat *Chat) error {
	now := time.Now()
	chat.UpdatedAt = now

	query := `
		INSERT INTO chats (jid, name, last_message_time, ephemeral_expiration, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(jid) DO UPDATE SET
			name = excluded.name,
			last_message_time = excluded.last_message_time,
			ephemeral_expiration = excluded.ephemeral_expiration,
			updated_at = excluded.updated_at
	`

	_, err := r.db.Exec(query, chat.JID, chat.Name, chat.LastMessageTime, chat.EphemeralExpiration, now, chat.UpdatedAt)
	return err
}

// GetChat retrieves a chat by JID
func (r *SQLiteRepository) GetChat(jid string) (*Chat, error) {
	chat := &Chat{}
	query := `
		SELECT jid, name, last_message_time, ephemeral_expiration, created_at, updated_at
		FROM chats
		WHERE jid = ?
	`

	err := r.db.QueryRow(query, jid).Scan(
		&chat.JID, &chat.Name, &chat.LastMessageTime, &chat.EphemeralExpiration,
		&chat.CreatedAt, &chat.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return chat, err
}

// GetChats retrieves chats with filtering
func (r *SQLiteRepository) GetChats(filter *ChatFilter) ([]*Chat, error) {
	var conditions []string
	var args []interface{}

	query := `
		SELECT c.jid, c.name, c.last_message_time, c.ephemeral_expiration, c.created_at, c.updated_at
		FROM chats c
	`

	if filter.SearchName != "" {
		conditions = append(conditions, "c.name LIKE ?")
		args = append(args, "%"+filter.SearchName+"%")
	}

	if filter.HasMedia {
		query += " INNER JOIN messages m ON c.jid = m.chat_jid"
		conditions = append(conditions, "m.media_type != ''")
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY c.last_message_time DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []*Chat
	for rows.Next() {
		chat := &Chat{}
		err := rows.Scan(
			&chat.JID, &chat.Name, &chat.LastMessageTime, &chat.EphemeralExpiration,
			&chat.CreatedAt, &chat.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}

	return chats, rows.Err()
}

// UpdateChatLastMessage updates the last message time for a chat
func (r *SQLiteRepository) UpdateChatLastMessage(jid string, lastMessageTime time.Time) error {
	query := `
		UPDATE chats 
		SET last_message_time = ?, updated_at = ?
		WHERE jid = ?
	`
	_, err := r.db.Exec(query, lastMessageTime, time.Now(), jid)
	return err
}

// DeleteChat deletes a chat and all its messages
func (r *SQLiteRepository) DeleteChat(jid string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete messages first (foreign key constraint)
	_, err = tx.Exec("DELETE FROM messages WHERE chat_jid = ?", jid)
	if err != nil {
		return err
	}

	// Delete chat
	_, err = tx.Exec("DELETE FROM chats WHERE jid = ?", jid)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// StoreMessage creates or updates a message
func (r *SQLiteRepository) StoreMessage(message *Message) error {
	now := time.Now()
	message.CreatedAt = now
	message.UpdatedAt = now

	// Skip empty messages
	if message.Content == "" && message.MediaType == "" {
		return nil
	}

	query := `
		INSERT INTO messages (
			id, chat_jid, sender, content, timestamp, is_from_me, 
			media_type, filename, url, media_key, file_sha256, 
			file_enc_sha256, file_length, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, chat_jid) DO UPDATE SET
			sender = excluded.sender,
			content = excluded.content,
			timestamp = excluded.timestamp,
			is_from_me = excluded.is_from_me,
			media_type = excluded.media_type,
			filename = excluded.filename,
			url = excluded.url,
			media_key = excluded.media_key,
			file_sha256 = excluded.file_sha256,
			file_enc_sha256 = excluded.file_enc_sha256,
			file_length = excluded.file_length,
			updated_at = excluded.updated_at
	`

	_, err := r.db.Exec(query,
		message.ID, message.ChatJID, message.Sender, message.Content,
		message.Timestamp, message.IsFromMe, message.MediaType, message.Filename,
		message.URL, message.MediaKey, message.FileSHA256, message.FileEncSHA256,
		message.FileLength, message.CreatedAt, message.UpdatedAt,
	)

	return err
}

// StoreMessagesBatch creates or updates multiple messages in a single transaction
func (r *SQLiteRepository) StoreMessagesBatch(messages []*Message) error {
	if len(messages) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare the statement once for better performance
	stmt, err := tx.Prepare(`
		INSERT INTO messages (
			id, chat_jid, sender, content, timestamp, is_from_me, 
			media_type, filename, url, media_key, file_sha256, 
			file_enc_sha256, file_length, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id, chat_jid) DO UPDATE SET
			sender = excluded.sender,
			content = excluded.content,
			timestamp = excluded.timestamp,
			is_from_me = excluded.is_from_me,
			media_type = excluded.media_type,
			filename = excluded.filename,
			url = excluded.url,
			media_key = excluded.media_key,
			file_sha256 = excluded.file_sha256,
			file_enc_sha256 = excluded.file_enc_sha256,
			file_length = excluded.file_length,
			updated_at = excluded.updated_at
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, message := range messages {
		// Skip empty messages
		if message.Content == "" && message.MediaType == "" {
			continue
		}

		message.CreatedAt = now
		message.UpdatedAt = now

		_, err = stmt.Exec(
			message.ID, message.ChatJID, message.Sender, message.Content,
			message.Timestamp, message.IsFromMe, message.MediaType, message.Filename,
			message.URL, message.MediaKey, message.FileSHA256, message.FileEncSHA256,
			message.FileLength, message.CreatedAt, message.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to store message %s: %w", message.ID, err)
		}
	}

	return tx.Commit()
}

// GetMessage retrieves a specific message
func (r *SQLiteRepository) GetMessage(id, chatJID string) (*Message, error) {
	message := &Message{}
	query := `
		SELECT id, chat_jid, sender, content, timestamp, is_from_me,
			media_type, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length, created_at, updated_at
		FROM messages
		WHERE id = ? AND chat_jid = ?
	`

	err := r.db.QueryRow(query, id, chatJID).Scan(
		&message.ID, &message.ChatJID, &message.Sender, &message.Content,
		&message.Timestamp, &message.IsFromMe, &message.MediaType, &message.Filename,
		&message.URL, &message.MediaKey, &message.FileSHA256, &message.FileEncSHA256,
		&message.FileLength, &message.CreatedAt, &message.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return message, err
}

// GetMessages retrieves messages with filtering
func (r *SQLiteRepository) GetMessages(filter *MessageFilter) ([]*Message, error) {
	var conditions []string
	var args []interface{}

	conditions = append(conditions, "chat_jid = ?")
	args = append(args, filter.ChatJID)

	if filter.StartTime != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, *filter.StartTime)
	}

	if filter.EndTime != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, *filter.EndTime)
	}

	if filter.MediaOnly {
		conditions = append(conditions, "media_type != ''")
	}

	if filter.IsFromMe != nil {
		conditions = append(conditions, "is_from_me = ?")
		args = append(args, *filter.IsFromMe)
	}

	query := `
		SELECT id, chat_jid, sender, content, timestamp, is_from_me,
			media_type, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length, created_at, updated_at
		FROM messages
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY timestamp DESC
	`

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
		if filter.Offset > 0 {
			query += fmt.Sprintf(" OFFSET %d", filter.Offset)
		}
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		message := &Message{}
		err := rows.Scan(
			&message.ID, &message.ChatJID, &message.Sender, &message.Content,
			&message.Timestamp, &message.IsFromMe, &message.MediaType, &message.Filename,
			&message.URL, &message.MediaKey, &message.FileSHA256, &message.FileEncSHA256,
			&message.FileLength, &message.CreatedAt, &message.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}

	return messages, rows.Err()
}

// GetMediaInfo retrieves media information for a message
func (r *SQLiteRepository) GetMediaInfo(id, chatJID string) (*MediaInfo, error) {
	info := &MediaInfo{
		MessageID: id,
		ChatJID:   chatJID,
	}

	query := `
		SELECT media_type, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length
		FROM messages
		WHERE id = ? AND chat_jid = ?
	`

	err := r.db.QueryRow(query, id, chatJID).Scan(
		&info.MediaType, &info.Filename, &info.URL, &info.MediaKey,
		&info.FileSHA256, &info.FileEncSHA256, &info.FileLength,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return info, err
}

// UpdateMediaInfo updates media information for a message
func (r *SQLiteRepository) UpdateMediaInfo(id, chatJID string, mediaInfo *MediaInfo) error {
	query := `
		UPDATE messages 
		SET url = ?, media_key = ?, file_sha256 = ?, 
			file_enc_sha256 = ?, file_length = ?, updated_at = ?
		WHERE id = ? AND chat_jid = ?
	`

	_, err := r.db.Exec(query,
		mediaInfo.URL, mediaInfo.MediaKey, mediaInfo.FileSHA256,
		mediaInfo.FileEncSHA256, mediaInfo.FileLength, time.Now(),
		id, chatJID,
	)

	return err
}

// DeleteMessage deletes a specific message
func (r *SQLiteRepository) DeleteMessage(id, chatJID string) error {
	_, err := r.db.Exec("DELETE FROM messages WHERE id = ? AND chat_jid = ?", id, chatJID)
	return err
}

// DeleteMessagesByChat deletes all messages for a chat
func (r *SQLiteRepository) DeleteMessagesByChat(chatJID string) error {
	_, err := r.db.Exec("DELETE FROM messages WHERE chat_jid = ?", chatJID)
	return err
}

// GetChatMessageCount returns the number of messages in a chat
func (r *SQLiteRepository) GetChatMessageCount(chatJID string) (int64, error) {
	var count int64
	err := r.db.QueryRow("SELECT COUNT(*) FROM messages WHERE chat_jid = ?", chatJID).Scan(&count)
	return count, err
}

// GetTotalMessageCount returns the total number of messages
func (r *SQLiteRepository) GetTotalMessageCount() (int64, error) {
	var count int64
	err := r.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count)
	return count, err
}
