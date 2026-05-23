package chatstorage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/proto/waE2E"
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

	// Try update first, then insert if no rows affected (cross-db compatible)
	result, err := r.db.Exec(`
		UPDATE chats SET name = ?, last_message_time = ?, ephemeral_expiration = ?, updated_at = ?, archived = ?
		WHERE jid = ? AND device_id = ?
	`, chat.Name, chat.LastMessageTime, chat.EphemeralExpiration, chat.UpdatedAt, chat.Archived, chat.JID, chat.DeviceID)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		_, err = r.db.Exec(`
			INSERT INTO chats (jid, device_id, name, last_message_time, ephemeral_expiration, created_at, updated_at, archived)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, chat.JID, chat.DeviceID, chat.Name, chat.LastMessageTime, chat.EphemeralExpiration, now, chat.UpdatedAt, chat.Archived)
	}
	return err
}

// GetChat retrieves a chat by JID
func (r *SQLiteRepository) GetChat(jid string) (*domainChatStorage.Chat, error) {
	query := `
		SELECT device_id, jid, name, last_message_time, ephemeral_expiration, created_at, updated_at, archived
		FROM chats
		WHERE jid = ?
	`

	chat, err := r.scanChat(r.db.QueryRow(query, jid))
	if err == sql.ErrNoRows {
		return nil, nil
	}

	return chat, err
}

// GetChatByDevice retrieves a chat by JID for a specific device
func (r *SQLiteRepository) GetChatByDevice(deviceID, jid string) (*domainChatStorage.Chat, error) {
	query := `
		SELECT device_id, jid, name, last_message_time, ephemeral_expiration, created_at, updated_at, archived
		FROM chats
		WHERE jid = ? AND device_id = ?
	`

	chat, err := r.scanChat(r.db.QueryRow(query, jid, deviceID))
	if err == sql.ErrNoRows {
		return nil, nil
	}

	return chat, err
}

// GetMessageByID retrieves a message by its ID from any chat
// This is more efficient than searching through all chats
func (r *SQLiteRepository) GetMessageByID(id string) (*domainChatStorage.Message, error) {
	query := `
		SELECT id, chat_jid, device_id, sender, content, timestamp, is_from_me,
			media_type, call_metadata, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length, referral_metadata, created_at, updated_at
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

// buildChatFilterQuery constructs the shared WHERE clause and JOIN for chat filter queries.
// Returns the query fragment (starting from JOIN/WHERE), conditions, and args.
func (r *SQLiteRepository) buildChatFilterQuery(filter *domainChatStorage.ChatFilter) (joinClause string, conditions []string, args []any) {
	if filter.SearchName != "" {
		conditions = append(conditions, "c.name LIKE ?")
		args = append(args, "%"+filter.SearchName+"%")
	}

	if filter.HasMedia {
		// EXISTS avoids duplicating chats when a conversation has multiple media messages (JOIN would).
		conditions = append(conditions, `EXISTS (SELECT 1 FROM messages m WHERE m.chat_jid = c.jid AND m.device_id = c.device_id AND m.media_type NOT IN ('', 'call'))`)
	}

	if filter.DeviceID != "" {
		conditions = append(conditions, "c.device_id = ?")
		args = append(args, filter.DeviceID)
	}

	if filter.IsArchived != nil {
		conditions = append(conditions, "c.archived = ?")
		if *filter.IsArchived {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}

	return joinClause, conditions, args
}

// GetChats retrieves chats with filtering
func (r *SQLiteRepository) GetChats(filter *domainChatStorage.ChatFilter) ([]*domainChatStorage.Chat, error) {
	query := `
		SELECT c.device_id, c.jid, c.name, c.last_message_time, c.ephemeral_expiration, c.created_at, c.updated_at, c.archived
		FROM chats c
	`

	joinClause, conditions, args := r.buildChatFilterQuery(filter)
	query += joinClause

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY c.last_message_time DESC"

	// Safely add LIMIT and OFFSET using parameterized values
	if filter.Limit > 0 {
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

	_, err = tx.Exec("DELETE FROM message_reactions WHERE chat_jid = ?", jid)
	if err != nil {
		return err
	}

	// Delete messages after reactions to keep cleanup explicit.
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

// DeleteChatByDevice deletes a chat and all its messages for a specific device
func (r *SQLiteRepository) DeleteChatByDevice(deviceID, jid string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM message_reactions WHERE chat_jid = ? AND device_id = ?", jid, deviceID)
	if err != nil {
		return err
	}

	// Delete messages after reactions to keep cleanup explicit.
	_, err = tx.Exec("DELETE FROM messages WHERE chat_jid = ? AND device_id = ?", jid, deviceID)
	if err != nil {
		return err
	}

	// Delete chat
	_, err = tx.Exec("DELETE FROM chats WHERE jid = ? AND device_id = ?", jid, deviceID)
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

	// Skip empty messages (allow synthetic rows with only media_type, e.g. call)
	if message.Content == "" && message.MediaType == "" {
		return nil
	}

	// Try update first, then insert if no rows affected (cross-db compatible)
	result, err := r.db.Exec(`
		UPDATE messages SET sender = ?, content = ?, timestamp = ?, is_from_me = ?,
			media_type = ?, call_metadata = ?, filename = ?, url = ?, media_key = ?, file_sha256 = ?,
			file_enc_sha256 = ?, file_length = ?, referral_metadata = ?, updated_at = ?
		WHERE id = ? AND chat_jid = ? AND device_id = ?
	`, message.Sender, message.Content, message.Timestamp, message.IsFromMe,
		message.MediaType, message.CallMetadata, message.Filename, message.URL, message.MediaKey, message.FileSHA256,
		message.FileEncSHA256, message.FileLength, message.ReferralMetadata, message.UpdatedAt,
		message.ID, message.ChatJID, message.DeviceID)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		_, err = r.db.Exec(`
			INSERT INTO messages (
				id, chat_jid, device_id, sender, content, timestamp, is_from_me,
				media_type, call_metadata, filename, url, media_key, file_sha256,
				file_enc_sha256, file_length, referral_metadata, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, message.ID, message.ChatJID, message.DeviceID, message.Sender, message.Content,
			message.Timestamp, message.IsFromMe, message.MediaType, message.CallMetadata, message.Filename,
			message.URL, message.MediaKey, message.FileSHA256, message.FileEncSHA256,
			message.FileLength, message.ReferralMetadata, message.CreatedAt, message.UpdatedAt)
	}
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

	// Prepare statements for update and insert
	updateStmt, err := tx.Prepare(`
		UPDATE messages SET sender = ?, content = ?, timestamp = ?, is_from_me = ?,
			media_type = ?, call_metadata = ?, filename = ?, url = ?, media_key = ?, file_sha256 = ?,
			file_enc_sha256 = ?, file_length = ?, referral_metadata = ?, updated_at = ?
		WHERE id = ? AND chat_jid = ? AND device_id = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer updateStmt.Close()

	insertStmt, err := tx.Prepare(`
		INSERT INTO messages (
			id, chat_jid, device_id, sender, content, timestamp, is_from_me,
			media_type, call_metadata, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length, referral_metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer insertStmt.Close()

	now := time.Now()
	for _, message := range messages {
		if message.Content == "" && message.MediaType == "" {
			continue
		}

		message.CreatedAt = now
		message.UpdatedAt = now

		result, err := updateStmt.Exec(
			message.Sender, message.Content, message.Timestamp, message.IsFromMe,
			message.MediaType, message.CallMetadata, message.Filename, message.URL, message.MediaKey, message.FileSHA256,
			message.FileEncSHA256, message.FileLength, message.ReferralMetadata, message.UpdatedAt,
			message.ID, message.ChatJID, message.DeviceID,
		)
		if err != nil {
			return fmt.Errorf("failed to update message %s: %w", message.ID, err)
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			_, err = insertStmt.Exec(
				message.ID, message.ChatJID, message.DeviceID, message.Sender, message.Content,
				message.Timestamp, message.IsFromMe, message.MediaType, message.CallMetadata, message.Filename,
				message.URL, message.MediaKey, message.FileSHA256, message.FileEncSHA256,
				message.FileLength, message.ReferralMetadata, message.CreatedAt, message.UpdatedAt,
			)
			if err != nil {
				return fmt.Errorf("failed to insert message %s: %w", message.ID, err)
			}
		}
	}

	return tx.Commit()
}

// StoreReaction creates, updates, or removes a message reaction.
func (r *SQLiteRepository) StoreReaction(reaction *domainChatStorage.Reaction) error {
	if reaction == nil {
		return nil
	}
	if reaction.MessageID == "" || reaction.ChatJID == "" || reaction.DeviceID == "" || reaction.ReactorJID == "" {
		return fmt.Errorf("reaction requires message_id, chat_jid, device_id, and reactor_jid")
	}
	if reaction.Emoji == "" {
		return r.DeleteReaction(reaction.MessageID, reaction.ReactorJID, reaction.DeviceID)
	}

	now := time.Now()
	if reaction.CreatedAt.IsZero() {
		reaction.CreatedAt = now
	}
	reaction.UpdatedAt = now

	result, err := r.db.Exec(`
		UPDATE message_reactions
		SET chat_jid = ?, emoji = ?, is_from_me = ?, reaction_timestamp = ?, updated_at = ?
		WHERE message_id = ? AND reactor_jid = ? AND device_id = ?
	`, reaction.ChatJID, reaction.Emoji, reaction.IsFromMe, reaction.Timestamp, reaction.UpdatedAt,
		reaction.MessageID, reaction.ReactorJID, reaction.DeviceID)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		_, err = r.db.Exec(`
			INSERT INTO message_reactions (
				message_id, chat_jid, device_id, reactor_jid, emoji, is_from_me,
				reaction_timestamp, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, reaction.MessageID, reaction.ChatJID, reaction.DeviceID, reaction.ReactorJID,
			reaction.Emoji, reaction.IsFromMe, reaction.Timestamp, reaction.CreatedAt, reaction.UpdatedAt)
	}
	return err
}

// DeleteReaction removes a single stored reaction.
func (r *SQLiteRepository) DeleteReaction(messageID, reactorJID, deviceID string) error {
	if messageID == "" || reactorJID == "" || deviceID == "" {
		return nil
	}

	_, err := r.db.Exec(`
		DELETE FROM message_reactions
		WHERE message_id = ? AND reactor_jid = ? AND device_id = ?
	`, messageID, reactorJID, deviceID)
	return err
}

// GetMessages retrieves messages with filtering
func (r *SQLiteRepository) GetMessages(filter *domainChatStorage.MessageFilter) ([]*domainChatStorage.Message, error) {
	// Require device_id for data isolation - fail fast if missing
	if filter.DeviceID == "" {
		return nil, fmt.Errorf("device_id is required for message queries (data isolation)")
	}

	var conditions []string
	var args []any

	conditions = append(conditions, "chat_jid = ?")
	args = append(args, filter.ChatJID)

	// Filter by device_id to ensure data isolation between devices
	conditions = append(conditions, "device_id = ?")
	args = append(args, filter.DeviceID)

	if filter.StartTime != nil {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, *filter.StartTime)
	}

	if filter.EndTime != nil {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, *filter.EndTime)
	}

	if filter.MediaOnly {
		conditions = append(conditions, "media_type NOT IN ('', 'call')")
	}

	if filter.IsFromMe != nil {
		conditions = append(conditions, "is_from_me = ?")
		args = append(args, *filter.IsFromMe)
	}

	query := `
		SELECT id, chat_jid, device_id, sender, content, timestamp, is_from_me,
			media_type, call_metadata, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length, referral_metadata, created_at, updated_at
		FROM messages
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY timestamp DESC
	`

	// Safely add LIMIT and OFFSET using parameterized values
	if filter.Limit > 0 {
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

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.loadMessageReactions(filter.DeviceID, filter.ChatJID, messages); err != nil {
		return nil, err
	}

	return messages, nil
}

// SearchMessages performs database-level search for messages containing specific text
func (r *SQLiteRepository) SearchMessages(deviceID, chatJID, searchText string, limit int) ([]*domainChatStorage.Message, error) {
	// Require device_id for data isolation - fail fast if missing
	if deviceID == "" {
		return nil, fmt.Errorf("device_id is required for message search (data isolation)")
	}

	if strings.TrimSpace(searchText) == "" {
		return []*domainChatStorage.Message{}, nil
	}

	var conditions []string
	var args []any

	conditions = append(conditions, "chat_jid = ?")
	args = append(args, chatJID)

	conditions = append(conditions, "device_id = ?")
	args = append(args, deviceID)

	// Add search condition using LIKE operator for case-insensitive search
	conditions = append(conditions, "LOWER(content) LIKE ?")
	args = append(args, "%"+strings.ToLower(searchText)+"%")

	query := `
		SELECT id, chat_jid, device_id, sender, content, timestamp, is_from_me,
			media_type, call_metadata, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length, referral_metadata, created_at, updated_at
		FROM messages
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY timestamp DESC
	`

	// Add limit with validation
	if limit > 0 {
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

	if err := r.loadMessageReactions(deviceID, chatJID, messages); err != nil {
		return nil, fmt.Errorf("failed to load message reactions: %w", err)
	}

	return messages, nil
}

func (r *SQLiteRepository) loadMessageReactions(deviceID, chatJID string, messages []*domainChatStorage.Message) error {
	if len(messages) == 0 {
		return nil
	}

	messageIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		if message != nil && message.ID != "" {
			messageIDs = append(messageIDs, message.ID)
		}
	}
	if len(messageIDs) == 0 {
		return nil
	}

	reactionsByMessageID := make(map[string][]domainChatStorage.Reaction)
	for start := 0; start < len(messageIDs); start += 500 {
		end := start + 500
		if end > len(messageIDs) {
			end = len(messageIDs)
		}

		batchIDs := messageIDs[start:end]
		placeholders := make([]string, 0, len(batchIDs))
		args := make([]any, 0, len(batchIDs)+2)
		args = append(args, deviceID, chatJID)
		for _, messageID := range batchIDs {
			placeholders = append(placeholders, "?")
			args = append(args, messageID)
		}

		query := `
			SELECT message_id, chat_jid, device_id, reactor_jid, emoji, is_from_me,
				reaction_timestamp, created_at, updated_at
			FROM message_reactions
			WHERE device_id = ? AND chat_jid = ? AND message_id IN (` + strings.Join(placeholders, ",") + `)
			ORDER BY reaction_timestamp ASC, created_at ASC
		`

		rows, err := r.db.Query(query, args...)
		if err != nil {
			return err
		}

		for rows.Next() {
			var reaction domainChatStorage.Reaction
			if err := rows.Scan(
				&reaction.MessageID, &reaction.ChatJID, &reaction.DeviceID, &reaction.ReactorJID,
				&reaction.Emoji, &reaction.IsFromMe, &reaction.Timestamp, &reaction.CreatedAt, &reaction.UpdatedAt,
			); err != nil {
				rows.Close()
				return err
			}
			reactionsByMessageID[reaction.MessageID] = append(reactionsByMessageID[reaction.MessageID], reaction)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}

	for _, message := range messages {
		if message == nil {
			continue
		}
		message.Reactions = reactionsByMessageID[message.ID]
	}

	return nil
}

// DeleteMessage deletes a specific message
func (r *SQLiteRepository) DeleteMessage(id, chatJID string) error {
	if _, err := r.db.Exec("DELETE FROM message_reactions WHERE message_id = ? AND chat_jid = ?", id, chatJID); err != nil {
		return err
	}
	_, err := r.db.Exec("DELETE FROM messages WHERE id = ? AND chat_jid = ?", id, chatJID)
	return err
}

// DeleteMessageByDevice deletes a specific message for a specific device
func (r *SQLiteRepository) DeleteMessageByDevice(deviceID, id, chatJID string) error {
	if _, err := r.db.Exec("DELETE FROM message_reactions WHERE message_id = ? AND chat_jid = ? AND device_id = ?", id, chatJID, deviceID); err != nil {
		return err
	}
	_, err := r.db.Exec("DELETE FROM messages WHERE id = ? AND chat_jid = ? AND device_id = ?", id, chatJID, deviceID)
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
		&message.ID, &message.ChatJID, &message.DeviceID, &message.Sender, &message.Content,
		&message.Timestamp, &message.IsFromMe, &message.MediaType, &message.CallMetadata, &message.Filename,
		&message.URL, &message.MediaKey, &message.FileSHA256, &message.FileEncSHA256,
		&message.FileLength, &message.ReferralMetadata, &message.CreatedAt, &message.UpdatedAt,
	)
	return message, err
}

// scanChat is a private helper for scanning chat rows
func (r *SQLiteRepository) scanChat(scanner interface{ Scan(...any) error }) (*domainChatStorage.Chat, error) {
	chat := &domainChatStorage.Chat{}
	err := scanner.Scan(
		&chat.DeviceID, &chat.JID, &chat.Name, &chat.LastMessageTime, &chat.EphemeralExpiration,
		&chat.CreatedAt, &chat.UpdatedAt, &chat.Archived,
	)
	return chat, err
}

// GetChatMessageCount returns the number of messages in a chat
func (r *SQLiteRepository) GetChatMessageCount(chatJID string) (int64, error) {
	return r.getCount("SELECT COUNT(*) FROM messages WHERE chat_jid = ?", chatJID)
}

// GetChatMessageCountByDevice returns the number of messages in a chat for a specific device
func (r *SQLiteRepository) GetChatMessageCountByDevice(deviceID, chatJID string) (int64, error) {
	return r.getCount("SELECT COUNT(*) FROM messages WHERE chat_jid = ? AND device_id = ?", chatJID, deviceID)
}

// GetTotalMessageCount returns the total number of messages
func (r *SQLiteRepository) GetTotalMessageCount() (int64, error) {
	return r.getCount("SELECT COUNT(*) FROM messages")
}

// GetTotalChatCount returns the total number of chats
func (r *SQLiteRepository) GetTotalChatCount() (int64, error) {
	return r.getCount("SELECT COUNT(*) FROM chats")
}

// GetFilteredChatCount returns the count of chats matching the given filter
func (r *SQLiteRepository) GetFilteredChatCount(filter *domainChatStorage.ChatFilter) (int64, error) {
	query := `SELECT COUNT(*) FROM chats c`

	joinClause, conditions, args := r.buildChatFilterQuery(filter)
	query += joinClause

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	return r.getCount(query, args...)
}

// TruncateAllChats deletes all chats from the database
// Note: Due to foreign key constraints, messages must be deleted first
func (r *SQLiteRepository) TruncateAllChats() error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM message_reactions")
	if err != nil {
		return fmt.Errorf("failed to delete message reactions: %w", err)
	}

	// Delete messages after reactions to keep cleanup explicit.
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

// DeleteDeviceData deletes all chats and messages for a specific device_id.
// Messages are deleted via foreign key cascade from chats.
func (r *SQLiteRepository) DeleteDeviceData(deviceID string) error {
	if deviceID == "" {
		return fmt.Errorf("device id is required")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM message_reactions WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("failed to delete device reactions: %w", err)
	}

	// Delete messages after reactions via direct device_id filter.
	if _, err := tx.Exec(`DELETE FROM messages WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("failed to delete device messages: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM chats WHERE device_id = ?", deviceID); err != nil {
		return fmt.Errorf("failed to delete device chats: %w", err)
	}

	return tx.Commit()
}

// SaveDeviceRecord upserts a device registration for persistence across restarts.
func (r *SQLiteRepository) SaveDeviceRecord(record *domainChatStorage.DeviceRecord) error {
	if record == nil || strings.TrimSpace(record.DeviceID) == "" {
		return fmt.Errorf("device record with id is required")
	}

	now := time.Now()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now

	// Try update first, then insert if no rows affected (cross-db compatible)
	result, err := r.db.Exec(`
		UPDATE devices SET display_name = ?, jid = ?, updated_at = ?
		WHERE device_id = ?
	`, record.DisplayName, record.JID, record.UpdatedAt, record.DeviceID)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		_, err = r.db.Exec(`
			INSERT INTO devices (device_id, display_name, jid, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
		`, record.DeviceID, record.DisplayName, record.JID, record.CreatedAt, record.UpdatedAt)
	}
	return err
}

// ListDeviceRecords returns all registered devices.
func (r *SQLiteRepository) ListDeviceRecords() ([]*domainChatStorage.DeviceRecord, error) {
	rows, err := r.db.Query(`
		SELECT device_id, display_name, jid, created_at, updated_at
		FROM devices
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*domainChatStorage.DeviceRecord
	for rows.Next() {
		var rec domainChatStorage.DeviceRecord
		if err := rows.Scan(&rec.DeviceID, &rec.DisplayName, &rec.JID, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		records = append(records, &rec)
	}

	return records, rows.Err()
}

// GetDeviceRecord fetches a device registration by id.
func (r *SQLiteRepository) GetDeviceRecord(deviceID string) (*domainChatStorage.DeviceRecord, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, fmt.Errorf("device id is required")
	}

	rec := &domainChatStorage.DeviceRecord{}
	err := r.db.QueryRow(`
		SELECT device_id, display_name, jid, created_at, updated_at
		FROM devices
		WHERE device_id = ?
		LIMIT 1
	`, deviceID).Scan(&rec.DeviceID, &rec.DisplayName, &rec.JID, &rec.CreatedAt, &rec.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return rec, nil
}

// DeleteDeviceRecord removes a device registration entry.
func (r *SQLiteRepository) DeleteDeviceRecord(deviceID string) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("device id is required")
	}
	_, err := r.db.Exec("DELETE FROM devices WHERE device_id = ?", deviceID)
	return err
}

// GetChatNameWithPushName determines the appropriate name for a chat with pushname support
func (r *SQLiteRepository) GetChatNameWithPushName(jid types.JID, chatJID string, senderUser string, pushName string) string {
	// First, check if chat already exists with a name
	existingChat, err := r.GetChat(chatJID)
	if err == nil && existingChat != nil && existingChat.Name != "" {
		// If we have a pushname and the existing name is just a phone number/JID user, update it
		if pushName != "" && (existingChat.Name == jid.ToNonAD().User || existingChat.Name == senderUser) {
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
		if pushName != "" && pushName != senderUser && pushName != jid.ToNonAD().User {
			name = pushName
		} else if senderUser != "" {
			name = senderUser
		} else {
			name = jid.ToNonAD().User
		}
	}

	return name
}

// GetChatNameWithPushNameByDevice determines the appropriate name for a chat with pushname support (device-scoped)
func (r *SQLiteRepository) GetChatNameWithPushNameByDevice(deviceID string, jid types.JID, chatJID string, senderUser string, pushName string) string {
	// Special handling for status@broadcast - always return "Status"
	if chatJID == "status@broadcast" || jid.String() == "status@broadcast" {
		return "Status"
	}

	// First, check if chat already exists with a name (device-scoped!)
	existingChat, err := r.GetChatByDevice(deviceID, chatJID)
	if err == nil && existingChat != nil && existingChat.Name != "" {
		// If we have a pushname and the existing name is just a phone number/JID user, update it
		if pushName != "" && (existingChat.Name == jid.ToNonAD().User || existingChat.Name == senderUser) {
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
		if pushName != "" && pushName != senderUser && pushName != jid.ToNonAD().User {
			name = pushName
		} else if senderUser != "" {
			name = senderUser
		} else {
			name = jid.ToNonAD().User
		}
	}

	return name
}

func (r *SQLiteRepository) CreateMessage(ctx context.Context, evt *events.Message) error {
	if evt == nil || evt.Message == nil {
		return nil
	}

	// Get WhatsApp client for LID resolution (device-scoped if present in context)
	client := whatsapp.ClientFromContext(ctx)
	deviceID := ""
	if inst, ok := whatsapp.DeviceFromContext(ctx); ok && inst != nil {
		deviceID = inst.JID()
		if deviceID == "" {
			deviceID = inst.ID()
		}
	}
	if deviceID == "" && client != nil && client.Store != nil && client.Store.ID != nil {
		deviceID = client.Store.ID.ToNonAD().String()
	}

	// Normalize chat and sender JIDs (convert @lid to @s.whatsapp.net)
	normalizedChatJID := whatsapp.NormalizeJIDFromLID(ctx, evt.Info.Chat, client)
	normalizedSender := whatsapp.NormalizeJIDFromLID(ctx, evt.Info.Sender, client)

	chatJID := normalizedChatJID.String()
	// Store the full sender JID (user@server) to ensure consistency between received and sent messages
	sender := normalizedSender.ToNonAD().String()

	// Get appropriate chat name using pushname if available (device-scoped)
	chatName := r.GetChatNameWithPushNameByDevice(deviceID, normalizedChatJID, chatJID, normalizedSender.User, evt.Info.PushName)

	// Get existing chat to preserve ephemeral_expiration and archived status if needed (device-scoped)
	existingChat, err := r.GetChatByDevice(deviceID, chatJID)
	if err != nil {
		return fmt.Errorf("failed to get existing chat: %w", err)
	}

	// Extract ephemeral expiration from incoming message
	ephemeralExpiration := utils.ExtractEphemeralExpiration(evt.Message)

	// Create or update chat
	chat := &domainChatStorage.Chat{
		DeviceID:        deviceID,
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

	// Preserve existing archived state
	if existingChat != nil {
		chat.Archived = existingChat.Archived
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

	var referralMetadata string
	if referral := utils.ExtractExternalAdReply(evt.Message); referral != nil {
		if jsonBytes, err := json.Marshal(referral); err == nil {
			referralMetadata = string(jsonBytes)
		}
	}

	message := &domainChatStorage.Message{
		ID:               evt.Info.ID,
		ChatJID:          chatJID,
		DeviceID:         deviceID,
		Sender:           sender,
		Content:          content,
		Timestamp:        evt.Info.Timestamp,
		IsFromMe:         evt.Info.IsFromMe,
		MediaType:        mediaType,
		Filename:         filename,
		URL:              url,
		MediaKey:         mediaKey,
		FileSHA256:       fileSHA256,
		FileEncSHA256:    fileEncSHA256,
		FileLength:       fileLength,
		ReferralMetadata: referralMetadata,
	}

	// Store the message
	return r.StoreMessage(message)
}

// CreateReaction stores or removes a reaction event as its own row in message_reactions.
func (r *SQLiteRepository) CreateReaction(ctx context.Context, evt *events.Message) error {
	reaction, err := r.reactionFromEvent(ctx, evt)
	if err != nil {
		return err
	}
	if reaction == nil {
		return nil
	}
	return r.StoreReaction(reaction)
}

func (r *SQLiteRepository) reactionFromEvent(ctx context.Context, evt *events.Message) (*domainChatStorage.Reaction, error) {
	if evt == nil || evt.Message == nil {
		return nil, nil
	}

	msg := utils.UnwrapMessage(evt.Message)
	reactionMessage := msg.GetReactionMessage()
	if reactionMessage == nil {
		return nil, nil
	}

	key := reactionMessage.GetKey()
	if key == nil || key.GetID() == "" {
		logrus.Debugf("Skipping reaction event %s - missing reacted message id", evt.Info.ID)
		return nil, nil
	}

	client := whatsapp.ClientFromContext(ctx)
	deviceID := ""
	if inst, ok := whatsapp.DeviceFromContext(ctx); ok && inst != nil {
		deviceID = inst.JID()
		if deviceID == "" {
			deviceID = inst.ID()
		}
	}
	if deviceID == "" && client != nil && client.Store != nil && client.Store.ID != nil {
		deviceID = client.Store.ID.ToNonAD().String()
	}
	if deviceID == "" {
		return nil, domainChatStorage.ErrMissingDeviceContext
	}

	chatJID := evt.Info.Chat
	if chatJID.IsEmpty() && key.GetRemoteJID() != "" {
		parsed, err := types.ParseJID(key.GetRemoteJID())
		if err != nil {
			return nil, fmt.Errorf("failed to parse reaction chat jid %q: %w", key.GetRemoteJID(), err)
		}
		chatJID = parsed
	}
	if chatJID.IsEmpty() {
		return nil, fmt.Errorf("reaction chat jid is required")
	}
	normalizedChatJID := whatsapp.NormalizeJIDFromLID(ctx, chatJID, client)

	reactorJID := evt.Info.Sender
	if reactorJID.IsEmpty() && evt.Info.IsFromMe && client != nil && client.Store != nil && client.Store.ID != nil {
		reactorJID = client.Store.ID.ToNonAD()
	}
	if reactorJID.IsEmpty() {
		return nil, fmt.Errorf("reaction sender jid is required")
	}
	normalizedReactorJID := whatsapp.NormalizeJIDFromLID(ctx, reactorJID, client)

	return &domainChatStorage.Reaction{
		MessageID:  key.GetID(),
		ChatJID:    normalizedChatJID.String(),
		DeviceID:   deviceID,
		ReactorJID: normalizedReactorJID.ToNonAD().String(),
		Emoji:      reactionMessage.GetText(),
		IsFromMe:   evt.Info.IsFromMe,
		Timestamp:  evt.Info.Timestamp,
	}, nil
}

// CreateIncomingCallRecord stores an incoming call as a synthetic message row (media_type "call").
func (r *SQLiteRepository) CreateIncomingCallRecord(ctx context.Context, evt *events.CallOffer, autoRejected bool) error {
	if evt == nil {
		return nil
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return domainChatStorage.ErrMissingDeviceContext
	}

	deviceID := client.Store.ID.ToNonAD().String()

	var peerJID types.JID
	if !evt.GroupJID.IsEmpty() {
		peerJID = evt.GroupJID
	} else {
		peerJID = evt.From
	}
	if peerJID.IsEmpty() {
		return fmt.Errorf("%w (call_id=%q group_jid=%s from=%s)",
			domainChatStorage.ErrCallOfferMissingPeerJID,
			evt.CallID,
			evt.GroupJID.String(),
			evt.From.String(),
		)
	}

	normalizedChat := whatsapp.NormalizeJIDFromLID(ctx, peerJID, client)
	chatJID := normalizedChat.String()

	normalizedCreator := whatsapp.NormalizeJIDFromLID(ctx, evt.CallCreator, client)
	sender := normalizedCreator.ToNonAD().String()
	if sender == "" {
		sender = evt.CallCreator.ToNonAD().String()
	}

	chatName := r.GetChatNameWithPushNameByDevice(deviceID, normalizedChat, chatJID, normalizedCreator.User, "")

	existingChat, err := r.GetChatByDevice(deviceID, chatJID)
	if err != nil {
		return fmt.Errorf("failed to get existing chat: %w", err)
	}

	chat := &domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            chatName,
		LastMessageTime: evt.Timestamp,
	}
	if existingChat != nil {
		chat.EphemeralExpiration = existingChat.EphemeralExpiration
		chat.Archived = existingChat.Archived
	}
	if err := r.StoreChat(chat); err != nil {
		return fmt.Errorf("failed to store chat for call: %w", err)
	}

	meta := map[string]any{
		"call_id":       evt.CallID,
		"auto_rejected": autoRejected,
	}
	if evt.RemotePlatform != "" {
		meta["remote_platform"] = evt.RemotePlatform
	}
	if evt.RemoteVersion != "" {
		meta["remote_version"] = evt.RemoteVersion
	}
	if !evt.GroupJID.IsEmpty() {
		meta["group_jid"] = evt.GroupJID.ToNonAD().String()
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal call metadata: %w", err)
	}

	msgID := "call:" + evt.CallID
	if evt.CallID == "" {
		msgID = fmt.Sprintf("call:%d", evt.Timestamp.UnixNano())
	}

	message := &domainChatStorage.Message{
		ID:           msgID,
		ChatJID:      chatJID,
		DeviceID:     deviceID,
		Sender:       sender,
		Content:      "Incoming call",
		Timestamp:    evt.Timestamp,
		IsFromMe:     false,
		MediaType:    "call",
		CallMetadata: string(metaBytes),
	}
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
func (r *SQLiteRepository) StoreSentMessageWithContext(ctx context.Context, messageID string, senderJID string, recipientJID string, content string, timestamp time.Time, msg *waE2E.Message) error {
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

	// Get WhatsApp client for LID resolution (device-scoped if present in context)
	client := whatsapp.ClientFromContext(ctx)
	deviceID := ""
	if inst, ok := whatsapp.DeviceFromContext(ctx); ok && inst != nil {
		deviceID = inst.JID()
		if deviceID == "" {
			deviceID = inst.ID()
		}
	}
	if deviceID == "" && client != nil && client.Store != nil && client.Store.ID != nil {
		deviceID = client.Store.ID.ToNonAD().String()
	}

	// Normalize recipient JID (convert @lid to @s.whatsapp.net)
	normalizedJID := whatsapp.NormalizeJIDFromLID(ctx, jid, client)
	chatJID := normalizedJID.String()

	// Get chat name (no pushname available for sent messages) - device scoped
	chatName := r.GetChatNameWithPushNameByDevice(deviceID, normalizedJID, chatJID, normalizedJID.User, "")

	// Check context again before database operations
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Get existing chat to preserve ephemeral_expiration and archived status (device-scoped)
	existingChat, err := r.GetChatByDevice(deviceID, chatJID)
	if err != nil {
		return fmt.Errorf("failed to get existing chat: %w", err)
	}

	// Store or update chat, preserving existing ephemeral_expiration
	chat := &domainChatStorage.Chat{
		DeviceID:        deviceID,
		JID:             chatJID,
		Name:            chatName,
		LastMessageTime: timestamp,
	}

	// Preserve existing ephemeral_expiration and archived state if chat exists
	if existingChat != nil {
		chat.EphemeralExpiration = existingChat.EphemeralExpiration
		chat.Archived = existingChat.Archived
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

	// Extract media info from the protobuf message if available
	var mediaType, filename, mediaURL string
	var mediaKey, fileSHA256, fileEncSHA256 []byte
	var fileLength uint64
	if msg != nil {
		mediaType, filename, mediaURL, mediaKey, fileSHA256, fileEncSHA256, fileLength = utils.ExtractMediaInfo(msg)
	}

	// Store the sent message
	message := &domainChatStorage.Message{
		ID:            messageID,
		ChatJID:       chatJID,
		DeviceID:      deviceID,
		Sender:        senderJID,
		Content:       content,
		Timestamp:     timestamp,
		IsFromMe:      true,
		MediaType:     mediaType,
		Filename:      filename,
		URL:           mediaURL,
		MediaKey:      mediaKey,
		FileSHA256:    fileSHA256,
		FileEncSHA256: fileEncSHA256,
		FileLength:    fileLength,
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
			version INTEGER PRIMARY KEY,
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

	// Execute migration (single statement)
	if _, err := tx.Exec(migration); err != nil {
		return err
	}

	// Update schema version - delete then insert for cross-db compatibility
	_, _ = tx.Exec("DELETE FROM schema_info WHERE version = ?", version)
	if _, err := tx.Exec("INSERT INTO schema_info (version) VALUES (?)", version); err != nil {
		return err
	}

	return tx.Commit()
}

// getMigrations returns all database migrations
// Compatible with SQLite, MySQL, and PostgreSQL
func (r *SQLiteRepository) getMigrations() []string {
	return []string{
		// Migration 1: Create chats table
		`CREATE TABLE IF NOT EXISTS chats (
			jid VARCHAR(255) NOT NULL,
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			name VARCHAR(255) NOT NULL,
			last_message_time TIMESTAMP NOT NULL,
			ephemeral_expiration INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (jid, device_id)
		)`,

		// Migration 2: Create messages table
		`CREATE TABLE IF NOT EXISTS messages (
			id VARCHAR(255) NOT NULL,
			chat_jid VARCHAR(255) NOT NULL,
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			sender VARCHAR(255) NOT NULL,
			content TEXT,
			timestamp TIMESTAMP NOT NULL,
			is_from_me BOOLEAN DEFAULT FALSE,
			media_type VARCHAR(50),
			filename VARCHAR(255),
			url TEXT,
			media_key BLOB,
			file_sha256 BLOB,
			file_enc_sha256 BLOB,
			file_length INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id, chat_jid, device_id)
		)`,

		// Migration 3: Create devices table
		`CREATE TABLE IF NOT EXISTS devices (
			device_id VARCHAR(255) PRIMARY KEY,
			display_name VARCHAR(255) DEFAULT '',
			jid VARCHAR(255) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		// Migration 4: Create indexes for messages
		`CREATE INDEX IF NOT EXISTS idx_messages_chat_jid ON messages(chat_jid)`,

		// Migration 5
		`CREATE INDEX IF NOT EXISTS idx_messages_device ON messages(device_id)`,

		// Migration 6
		`CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp)`,

		// Migration 7
		`CREATE INDEX IF NOT EXISTS idx_messages_media_type ON messages(media_type)`,

		// Migration 8
		`CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender)`,

		// Migration 9: Create indexes for chats
		`CREATE INDEX IF NOT EXISTS idx_chats_last_message ON chats(last_message_time)`,

		// Migration 10
		`CREATE INDEX IF NOT EXISTS idx_chats_name ON chats(name)`,

		// Migration 11
		`CREATE INDEX IF NOT EXISTS idx_chats_device ON chats(device_id)`,

		// Migration 12: Create index for devices
		`CREATE INDEX IF NOT EXISTS idx_devices_created_at ON devices(created_at)`,

		// Migration 13: Add archived column to chats
		`ALTER TABLE chats ADD COLUMN archived BOOLEAN DEFAULT FALSE;`,

		// Migration 14: Add index for archived column
		`CREATE INDEX IF NOT EXISTS idx_chats_archived ON chats(archived)`,

		// Migration 15: JSON metadata for synthetic call rows (media_type = call)
		`ALTER TABLE messages ADD COLUMN call_metadata TEXT DEFAULT ''`,

		// Migration 16: JSON metadata for Meta Ads referral/attribution (CTWA)
		`ALTER TABLE messages ADD COLUMN referral_metadata TEXT DEFAULT ''`,

		// Migration 17: Store emoji reactions per message
		`CREATE TABLE IF NOT EXISTS message_reactions (
			message_id VARCHAR(255) NOT NULL,
			chat_jid VARCHAR(255) NOT NULL,
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			reactor_jid VARCHAR(255) NOT NULL,
			emoji TEXT NOT NULL DEFAULT '',
			is_from_me BOOLEAN DEFAULT FALSE,
			reaction_timestamp TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (message_id, reactor_jid, device_id)
		)`,

		// Migration 18: Index reactions by message and chat for history hydration
		`CREATE INDEX IF NOT EXISTS idx_message_reactions_lookup ON message_reactions(device_id, chat_jid, message_id)`,
	}
}
