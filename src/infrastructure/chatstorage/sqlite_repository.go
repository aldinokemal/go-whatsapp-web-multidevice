package chatstorage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// SQLiteRepository implements Repository using SQLite
type SQLiteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository creates a new SQLite repository
func NewStorageRepository(db *sql.DB) domainChatStorage.IChatStorageRepository {
	return &SQLiteRepository{db: db}
}

// StoreChat creates or updates a chat
func (r *SQLiteRepository) StoreChat(chat *domainChatStorage.Chat) error {
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
func (r *SQLiteRepository) GetChat(jid string) (*domainChatStorage.Chat, error) {
	query := `
		SELECT jid, name, last_message_time, ephemeral_expiration, created_at, updated_at
		FROM chats
		WHERE jid = ?
	`

	chat, err := r.scanChat(r.db.QueryRow(query, jid))
	if err == sql.ErrNoRows {
		return nil, nil
	}

	return chat, err
}

// GetMessageByID retrieves a message by its ID from any chat
// This is more efficient than searching through all chats
func (r *SQLiteRepository) GetMessageByID(id string) (*domainChatStorage.Message, error) {
	query := `
		SELECT id, chat_jid, sender, content, timestamp, is_from_me,
			media_type, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length, created_at, updated_at
		FROM messages
		WHERE id = ?
		LIMIT 1
	`

	message, err := r.scanMessage(r.db.QueryRow(query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}

	return message, err
}

// GetChats retrieves chats with filtering
func (r *SQLiteRepository) GetChats(filter *domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	var conditions []string
	var args []any

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

	// Safely add LIMIT and OFFSET using parameterized values
	if filter.Limit > 0 {
		// Validate limit to prevent abuse
		if filter.Limit > 1000 {
			filter.Limit = 1000
		}
		query += " LIMIT ?"
		args = append(args, filter.Limit)

		if filter.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, filter.Offset)
		}
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []*domainChatStorage.Chat
	for rows.Next() {
		chat, err := r.scanChat(rows)
		if err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}

	return chats, rows.Err()
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
func (r *SQLiteRepository) StoreMessage(message *domainChatStorage.Message) error {
	now := time.Now()
	message.CreatedAt = now
	message.UpdatedAt = now

	// Skip empty messages
	if message.Content == "" && message.MediaType == "" {
		// This is not an error, just skip storing empty messages
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
func (r *SQLiteRepository) StoreMessagesBatch(messages []*domainChatStorage.Message) error {
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

// GetMessages retrieves messages with filtering
func (r *SQLiteRepository) GetMessages(filter *domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	var conditions []string
	var args []any

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

	// Safely add LIMIT and OFFSET using parameterized values
	if filter.Limit > 0 {
		// Validate limit to prevent abuse
		if filter.Limit > 1000 {
			filter.Limit = 1000
		}
		query += " LIMIT ?"
		args = append(args, filter.Limit)

		if filter.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, filter.Offset)
		}
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*domainChatStorage.Message
	for rows.Next() {
		message, err := r.scanMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}

	return messages, rows.Err()
}

// SearchMessages performs database-level search for messages containing specific text
func (r *SQLiteRepository) SearchMessages(chatJID, searchText string, limit int) ([]*domainChatStorage.Message, error) {
	// Return empty results for empty search text
	if strings.TrimSpace(searchText) == "" {
		return []*domainChatStorage.Message{}, nil
	}

	var conditions []string
	var args []any

	// Always filter by chat JID
	conditions = append(conditions, "chat_jid = ?")
	args = append(args, chatJID)

	// Add search condition using LIKE operator for case-insensitive search
	conditions = append(conditions, "LOWER(content) LIKE ?")
	args = append(args, "%"+strings.ToLower(searchText)+"%")

	query := `
		SELECT id, chat_jid, sender, content, timestamp, is_from_me,
			media_type, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length, created_at, updated_at
		FROM messages
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY timestamp DESC
	`

	// Add limit with validation
	if limit > 0 {
		// Validate limit to prevent abuse
		if limit > 1000 {
			limit = 1000
		}
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to search messages: %w", err)
	}
	defer rows.Close()

	var messages []*domainChatStorage.Message
	for rows.Next() {
		message, err := r.scanMessage(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, message)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating messages: %w", err)
	}

	return messages, nil
}

// DeleteMessage deletes a specific message
func (r *SQLiteRepository) DeleteMessage(id, chatJID string) error {
	_, err := r.db.Exec("DELETE FROM messages WHERE id = ? AND chat_jid = ?", id, chatJID)
	return err
}

// getCount is a private helper for count queries
func (r *SQLiteRepository) getCount(query string, args ...any) (int64, error) {
	var count int64
	err := r.db.QueryRow(query, args...).Scan(&count)
	return count, err
}

// scanMessage is a private helper for scanning message rows
func (r *SQLiteRepository) scanMessage(scanner interface{ Scan(...any) error }) (*domainChatStorage.Message, error) {
	message := &domainChatStorage.Message{}
	err := scanner.Scan(
		&message.ID, &message.ChatJID, &message.Sender, &message.Content,
		&message.Timestamp, &message.IsFromMe, &message.MediaType, &message.Filename,
		&message.URL, &message.MediaKey, &message.FileSHA256, &message.FileEncSHA256,
		&message.FileLength, &message.CreatedAt, &message.UpdatedAt,
	)
	return message, err
}

// scanChat is a private helper for scanning chat rows
func (r *SQLiteRepository) scanChat(scanner interface{ Scan(...any) error }) (*domainChatStorage.Chat, error) {
	chat := &domainChatStorage.Chat{}
	err := scanner.Scan(
		&chat.JID, &chat.Name, &chat.LastMessageTime, &chat.EphemeralExpiration,
		&chat.CreatedAt, &chat.UpdatedAt,
	)
	return chat, err
}

// GetChatMessageCount returns the number of messages in a chat
func (r *SQLiteRepository) GetChatMessageCount(chatJID string) (int64, error) {
	return r.getCount("SELECT COUNT(*) FROM messages WHERE chat_jid = ?", chatJID)
}

// GetTotalMessageCount returns the total number of messages
func (r *SQLiteRepository) GetTotalMessageCount() (int64, error) {
	return r.getCount("SELECT COUNT(*) FROM messages")
}

// GetTotalChatCount returns the total number of chats
func (r *SQLiteRepository) GetTotalChatCount() (int64, error) {
	return r.getCount("SELECT COUNT(*) FROM chats")
}

// TruncateAllChats deletes all chats from the database
// Note: Due to foreign key constraints, messages must be deleted first
func (r *SQLiteRepository) TruncateAllChats() error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete messages first (foreign key constraint)
	_, err = tx.Exec("DELETE FROM messages")
	if err != nil {
		return fmt.Errorf("failed to delete messages: %w", err)
	}

	// Delete chats
	_, err = tx.Exec("DELETE FROM chats")
	if err != nil {
		return fmt.Errorf("failed to delete chats: %w", err)
	}

	return tx.Commit()
}

// GetChatNameWithPushName determines the appropriate name for a chat with pushname support
func (r *SQLiteRepository) GetChatNameWithPushName(jid types.JID, chatJID string, senderUser string, pushName string) string {
	// First, check if chat already exists with a name
	existingChat, err := r.GetChat(chatJID)
	if err == nil && existingChat != nil && existingChat.Name != "" {
		// If we have a pushname and the existing name is just a phone number/JID user, update it
		if pushName != "" && (existingChat.Name == jid.User || existingChat.Name == senderUser) {
			return pushName
		}
		return existingChat.Name
	}

	// Determine chat type and name
	var name string

	switch jid.Server {
	case "g.us":
		// This is a group chat
		// For now, use a generic name - this can be enhanced later with group info
		name = fmt.Sprintf("Group %s", jid.User)
	case "newsletter":
		// This is a newsletter/channel
		name = fmt.Sprintf("Newsletter %s", jid.User)
	default:
		// This is an individual contact
		// Priority: pushName > senderUser > JID user
		if pushName != "" && pushName != senderUser && pushName != jid.User {
			name = pushName
		} else if senderUser != "" {
			name = senderUser
		} else {
			name = jid.User
		}
	}

	return name
}

func (r *SQLiteRepository) CreateMessage(ctx context.Context, evt *events.Message) error {
	if evt == nil || evt.Message == nil {
		return nil
	}

	// Extract chat and sender information
	chatJID := evt.Info.Chat.String()
	// Store the full sender JID (user@server) to ensure consistency between received and sent messages
	sender := evt.Info.Sender.String()

	// Get appropriate chat name using pushname if available
	chatName := r.GetChatNameWithPushName(evt.Info.Chat, chatJID, evt.Info.Sender.User, evt.Info.PushName)

	// Get existing chat to preserve ephemeral_expiration if needed
	existingChat, err := r.GetChat(chatJID)
	if err != nil {
		return fmt.Errorf("failed to get existing chat: %w", err)
	}

	// Extract ephemeral expiration from incoming message
	ephemeralExpiration := utils.ExtractEphemeralExpiration(evt.Message)

	// Create or update chat
	chat := &domainChatStorage.Chat{
		JID:             chatJID,
		Name:            chatName,
		LastMessageTime: evt.Info.Timestamp,
	}

	// Set ephemeral expiration: use incoming message value if > 0, otherwise preserve existing
	if ephemeralExpiration > 0 {
		chat.EphemeralExpiration = ephemeralExpiration
	} else if existingChat != nil {
		// Preserve existing ephemeral_expiration if incoming message doesn't have one
		chat.EphemeralExpiration = existingChat.EphemeralExpiration
	}

	// Store or update the chat
	if err := r.StoreChat(chat); err != nil {
		return fmt.Errorf("failed to store chat: %w", err)
	}

	// Extract message content and media info
	content := utils.ExtractMessageTextFromProto(evt.Message)
	mediaType, filename, url, mediaKey, fileSHA256, fileEncSHA256, fileLength := utils.ExtractMediaInfo(evt.Message)

	// Skip if there's no content and no media
	if content == "" && mediaType == "" {
		logrus.Debugf("Skipping message %s - no content or media", evt.Info.ID)
		return nil
	}

	// Create message object
	message := &domainChatStorage.Message{
		ID:            evt.Info.ID,
		ChatJID:       chatJID,
		Sender:        sender,
		Content:       content,
		Timestamp:     evt.Info.Timestamp,
		IsFromMe:      evt.Info.IsFromMe,
		MediaType:     mediaType,
		Filename:      filename,
		URL:           url,
		MediaKey:      mediaKey,
		FileSHA256:    fileSHA256,
		FileEncSHA256: fileEncSHA256,
		FileLength:    fileLength,
	}

	// Store the message
	return r.StoreMessage(message)
}

// GetStorageStatistics returns current storage statistics for logging purposes
func (r *SQLiteRepository) GetStorageStatistics() (chatCount int64, messageCount int64, err error) {
	// Count all chats using efficient query
	chatCount, err = r.GetTotalChatCount()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get chat count: %w", err)
	}

	// Count all messages
	messageCount, err = r.GetTotalMessageCount()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get message count: %w", err)
	}

	return chatCount, messageCount, nil
}

// TruncateAllDataWithLogging performs truncation with detailed logging
func (r *SQLiteRepository) TruncateAllDataWithLogging(logPrefix string) error {
	// Get statistics before truncation
	chatCount, messageCount, err := r.GetStorageStatistics()
	if err != nil {
		logrus.Warnf("[%s] Failed to get storage statistics before truncation: %v", logPrefix, err)
	} else {
		logrus.Infof("[%s] Storage before truncation: %d chats, %d messages", logPrefix, chatCount, messageCount)
	}

	// Perform truncation
	if err := r.TruncateAllChats(); err != nil {
		return fmt.Errorf("failed to truncate chatstorage data: %w", err)
	}

	// Verify truncation
	chatCountAfter, messageCountAfter, err := r.GetStorageStatistics()
	if err != nil {
		logrus.Warnf("[%s] Failed to get storage statistics after truncation: %v", logPrefix, err)
	} else {
		logrus.Infof("[%s] Storage after truncation: %d chats, %d messages", logPrefix, chatCountAfter, messageCountAfter)
		if chatCountAfter == 0 && messageCountAfter == 0 {
			logrus.Infof("[%s] ✅ Chatstorage truncation completed successfully", logPrefix)
		} else {
			logrus.Warnf("[%s] ⚠️ Truncation may not have completed fully", logPrefix)
		}
	}

	return nil
}

// StoreSentMessageWithContext stores a message that was sent by the user with context cancellation support
func (r *SQLiteRepository) StoreSentMessageWithContext(ctx context.Context, messageID string, senderJID string, recipientJID string, content string, timestamp time.Time) error {
	// Check if context is already cancelled before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Ensure JID is properly formatted
	jid, err := types.ParseJID(recipientJID)
	if err != nil {
		return fmt.Errorf("invalid JID format: %w", err)
	}

	chatJID := jid.String()

	// Get chat name (no pushname available for sent messages)
	chatName := r.GetChatNameWithPushName(jid, chatJID, jid.User, "")

	// Check context again before database operations
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Get existing chat to preserve ephemeral_expiration
	existingChat, err := r.GetChat(chatJID)
	if err != nil {
		return fmt.Errorf("failed to get existing chat: %w", err)
	}

	// Store or update chat, preserving existing ephemeral_expiration
	chat := &domainChatStorage.Chat{
		JID:             chatJID,
		Name:            chatName,
		LastMessageTime: timestamp,
	}

	// Preserve existing ephemeral_expiration if chat exists
	if existingChat != nil {
		chat.EphemeralExpiration = existingChat.EphemeralExpiration
	}

	if err := r.StoreChat(chat); err != nil {
		return fmt.Errorf("failed to store chat: %w", err)
	}

	// Check context one more time before storing message
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Store the sent message
	message := &domainChatStorage.Message{
		ID:        messageID,
		ChatJID:   chatJID,
		Sender:    senderJID,
		Content:   content,
		Timestamp: timestamp,
		IsFromMe:  true,
	}

	return r.StoreMessage(message)
}

// _____________________________________________________________________________________________________________________

// initializeSchema creates or migrates the database schema
func (r *SQLiteRepository) InitializeSchema() error {
	// Get current schema version
	version, err := r.getSchemaVersion()
	if err != nil {
		return err
	}

	// Run migrations based on version
	migrations := r.getMigrations()
	for i := version; i < len(migrations); i++ {
		if err := r.runMigration(migrations[i], i+1); err != nil {
			return fmt.Errorf("failed to run migration %d: %w", i+1, err)
		}
	}

	return nil
}

// getSchemaVersion returns the current schema version
func (r *SQLiteRepository) getSchemaVersion() (int, error) {
	// Create schema_info table if it doesn't exist
	_, err := r.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_info (
			version INTEGER PRIMARY KEY DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return 0, err
	}

	// Get current version
	var version int
	err = r.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_info").Scan(&version)
	if err != nil {
		return 0, err
	}

	return version, nil
}

// runMigration executes a migration
func (r *SQLiteRepository) runMigration(migration string, version int) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Execute migration
	if _, err := tx.Exec(migration); err != nil {
		return err
	}

	// Update schema version
	if _, err := tx.Exec("INSERT OR REPLACE INTO schema_info (version) VALUES (?)", version); err != nil {
		return err
	}

	return tx.Commit()
}

// getMigrations returns all database migrations
func (r *SQLiteRepository) getMigrations() []string {
	return []string{
		// Migration 1: Initial schema with only chats and messages tables
		`
		-- Create chats table
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			last_message_time TIMESTAMP NOT NULL,
			ephemeral_expiration INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Create messages table
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT NOT NULL,
			chat_jid TEXT NOT NULL,
			sender TEXT NOT NULL,
			content TEXT,
			timestamp TIMESTAMP NOT NULL,
			is_from_me BOOLEAN DEFAULT FALSE,
			media_type TEXT,
			filename TEXT,
			url TEXT,
			media_key BLOB,
			file_sha256 BLOB,
			file_enc_sha256 BLOB,
			file_length INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES chats(jid) ON DELETE CASCADE
		);

		-- Create indexes for performance
		CREATE INDEX IF NOT EXISTS idx_messages_chat_jid ON messages(chat_jid);
		CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
		CREATE INDEX IF NOT EXISTS idx_messages_media_type ON messages(media_type);
		CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender);
		CREATE INDEX IF NOT EXISTS idx_chats_last_message ON chats(last_message_time);
		CREATE INDEX IF NOT EXISTS idx_chats_name ON chats(name);
		`,

		// Migration 2: Add index for message ID lookups (performance optimization)
		`
		CREATE INDEX IF NOT EXISTS idx_messages_id ON messages(id);
		`,
	}
}
