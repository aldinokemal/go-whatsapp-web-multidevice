package chatstorage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
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

	// Try update first, then insert if no rows affected (cross-db compatible)
	result, err := r.db.Exec(`
		UPDATE chats SET name = ?, last_message_time = ?, ephemeral_expiration = ?, updated_at = ?
		WHERE jid = ? AND device_id = ?
	`, chat.Name, chat.LastMessageTime, chat.EphemeralExpiration, chat.UpdatedAt, chat.JID, chat.DeviceID)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		_, err = r.db.Exec(`
			INSERT INTO chats (jid, device_id, name, last_message_time, ephemeral_expiration, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, chat.JID, chat.DeviceID, chat.Name, chat.LastMessageTime, chat.EphemeralExpiration, now, chat.UpdatedAt)
	}
	return err
}

// GetChat retrieves a chat by JID
func (r *SQLiteRepository) GetChat(jid string) (*domainChatStorage.Chat, error) {
	query := `
		SELECT device_id, jid, name, last_message_time, ephemeral_expiration, created_at, updated_at
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
		SELECT device_id, jid, name, last_message_time, ephemeral_expiration, created_at, updated_at
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
		SELECT c.device_id, c.jid, c.name, c.last_message_time, c.ephemeral_expiration, c.created_at, c.updated_at
		FROM chats c
	`

	if filter.SearchName != "" {
		conditions = append(conditions, "c.name LIKE ?")
		args = append(args, "%"+filter.SearchName+"%")
	}

	if filter.HasMedia {
		query += " INNER JOIN messages m ON c.jid = m.chat_jid AND c.device_id = m.device_id"
		conditions = append(conditions, "m.media_type != ''")
	}

	if filter.DeviceID != "" {
		conditions = append(conditions, "c.device_id = ?")
		args = append(args, filter.DeviceID)
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

// DeleteChatByDevice deletes a chat and all its messages for a specific device
func (r *SQLiteRepository) DeleteChatByDevice(deviceID, jid string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete messages first (foreign key constraint)
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

	// Skip empty messages
	if message.Content == "" && message.MediaType == "" {
		return nil
	}

	// Try update first, then insert if no rows affected (cross-db compatible)
	result, err := r.db.Exec(`
		UPDATE messages SET sender = ?, content = ?, timestamp = ?, is_from_me = ?,
			media_type = ?, filename = ?, url = ?, media_key = ?, file_sha256 = ?,
			file_enc_sha256 = ?, file_length = ?, updated_at = ?
		WHERE id = ? AND chat_jid = ? AND device_id = ?
	`, message.Sender, message.Content, message.Timestamp, message.IsFromMe,
		message.MediaType, message.Filename, message.URL, message.MediaKey, message.FileSHA256,
		message.FileEncSHA256, message.FileLength, message.UpdatedAt,
		message.ID, message.ChatJID, message.DeviceID)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		_, err = r.db.Exec(`
			INSERT INTO messages (
				id, chat_jid, device_id, sender, content, timestamp, is_from_me,
				media_type, filename, url, media_key, file_sha256,
				file_enc_sha256, file_length, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, message.ID, message.ChatJID, message.DeviceID, message.Sender, message.Content,
			message.Timestamp, message.IsFromMe, message.MediaType, message.Filename,
			message.URL, message.MediaKey, message.FileSHA256, message.FileEncSHA256,
			message.FileLength, message.CreatedAt, message.UpdatedAt)
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
			media_type = ?, filename = ?, url = ?, media_key = ?, file_sha256 = ?,
			file_enc_sha256 = ?, file_length = ?, updated_at = ?
		WHERE id = ? AND chat_jid = ? AND device_id = ?
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer updateStmt.Close()

	insertStmt, err := tx.Prepare(`
		INSERT INTO messages (
			id, chat_jid, device_id, sender, content, timestamp, is_from_me,
			media_type, filename, url, media_key, file_sha256,
			file_enc_sha256, file_length, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			message.MediaType, message.Filename, message.URL, message.MediaKey, message.FileSHA256,
			message.FileEncSHA256, message.FileLength, message.UpdatedAt,
			message.ID, message.ChatJID, message.DeviceID,
		)
		if err != nil {
			return fmt.Errorf("failed to update message %s: %w", message.ID, err)
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			_, err = insertStmt.Exec(
				message.ID, message.ChatJID, message.DeviceID, message.Sender, message.Content,
				message.Timestamp, message.IsFromMe, message.MediaType, message.Filename,
				message.URL, message.MediaKey, message.FileSHA256, message.FileEncSHA256,
				message.FileLength, message.CreatedAt, message.UpdatedAt,
			)
			if err != nil {
				return fmt.Errorf("failed to insert message %s: %w", message.ID, err)
			}
		}
	}

	return tx.Commit()
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
		conditions = append(conditions, "media_type != ''")
	}

	if filter.IsFromMe != nil {
		conditions = append(conditions, "is_from_me = ?")
		args = append(args, *filter.IsFromMe)
	}

	query := `
		SELECT id, chat_jid, device_id, sender, content, timestamp, is_from_me,
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

// DeleteMessageByDevice deletes a specific message for a specific device
func (r *SQLiteRepository) DeleteMessageByDevice(deviceID, id, chatJID string) error {
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
		&chat.DeviceID, &chat.JID, &chat.Name, &chat.LastMessageTime, &chat.EphemeralExpiration,
		&chat.CreatedAt, &chat.UpdatedAt,
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

	// Delete messages first via direct device_id filter
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

	// Get appropriate chat name using pushname if available
	chatName := r.GetChatNameWithPushName(normalizedChatJID, chatJID, normalizedSender.User, evt.Info.PushName)

	// Get existing chat to preserve ephemeral_expiration if needed
	existingChat, err := r.GetChat(chatJID)
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
		DeviceID:      deviceID,
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

	// Get chat name (no pushname available for sent messages)
	chatName := r.GetChatNameWithPushName(normalizedJID, chatJID, normalizedJID.User, "")

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
		DeviceID:        deviceID,
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
		DeviceID:  deviceID,
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
	// Execute migration (single statement)
	if _, err := r.db.Exec(migration); err != nil {
		return err
	}

	// Update schema version - delete then insert for cross-db compatibility
	_, _ = r.db.Exec("DELETE FROM schema_info WHERE version = ?", version)
	if _, err := r.db.Exec("INSERT INTO schema_info (version) VALUES (?)", version); err != nil {
		return err
	}

	return nil
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
	}
}
