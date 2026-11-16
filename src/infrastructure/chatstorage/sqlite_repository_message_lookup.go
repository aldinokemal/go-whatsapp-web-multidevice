package chatstorage

import (
	"database/sql"
	"fmt"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// GetMessageByIDChatAndDevice retrieves a message by its full storage identity.
// The messages table is keyed by (id, chat_jid, device_id), so callers that
// already know the chat must include it to avoid selecting a colliding ID from
// another conversation on the same device.
func (r *SQLiteRepository) GetMessageByIDChatAndDevice(deviceID, chatJID, id string) (*domainChatStorage.Message, error) {
	query := `
		SELECT id, chat_jid, device_id, sender, content, timestamp, is_from_me,
			media_type, call_metadata, filename, url, direct_path, media_key, file_sha256,
			file_enc_sha256, file_length, referral_metadata, context_metadata, created_at, updated_at
		FROM messages
		WHERE id = ? AND chat_jid = ? AND device_id = ?
		LIMIT 1
	`

	message, err := r.scanMessage(r.db.QueryRow(query, id, chatJID, deviceID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return message, err
}

// GetOldestMessageByDevice returns the earliest stored message for a chat,
// scoped to the device for data isolation. Used to anchor on-demand history
// sync requests, which need the oldest known message ID/timestamp to ask the
// phone for anything older. Returns (nil, nil) when the chat has no stored
// messages yet.
//
// Locally synthesized rows (e.g. incoming calls, stored as media_type "call"
// with id "call:<callID>") are excluded: the phone only recognizes real
// WebMessageInfo IDs as history-sync anchors, so a call row would repeatedly
// produce an invalid oldest_msg_id.
func (r *SQLiteRepository) GetOldestMessageByDevice(deviceID, chatJID string) (*domainChatStorage.Message, error) {
	query := `
		SELECT id, chat_jid, device_id, sender, content, timestamp, is_from_me,
			media_type, call_metadata, filename, url, direct_path, media_key, file_sha256,
			file_enc_sha256, file_length, referral_metadata, context_metadata, created_at, updated_at
		FROM messages
		WHERE chat_jid = ? AND device_id = ? AND (media_type IS NULL OR media_type != 'call')
		ORDER BY timestamp ASC, id ASC
		LIMIT 1
	`

	message, err := r.scanMessage(r.db.QueryRow(query, chatJID, deviceID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return message, err
}

// GetLatestMessageByDevice returns the newest stored message by instant across
// the given chat JIDs (for example the PN and LID forms of one chat), scoped to
// the device. Stored timestamps can carry different UTC offsets, so text order
// is not instant order: every candidate row is compared in Go. Only id,
// chat_jid, timestamp, is_from_me, and sender are set. Returns (nil, nil) when
// no message is stored, and an error for an unreadable timestamp.
func (r *SQLiteRepository) GetLatestMessageByDevice(deviceID string, chatJIDs ...string) (*domainChatStorage.Message, error) {
	if deviceID == "" || len(chatJIDs) == 0 {
		return nil, fmt.Errorf("device_id and chat JIDs are required for latest message lookup")
	}
	var latest *domainChatStorage.Message
	var latestRow int64
	for _, chatJID := range chatJIDs {
		// Bare columns, so both SQLite drivers scan the timestamp as time.Time.
		rows, err := r.db.Query("SELECT rowid, id, chat_jid, timestamp, is_from_me, sender FROM messages WHERE chat_jid = ? AND device_id = ?", chatJID, deviceID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var rowID int64
			var ts time.Time
			message := &domainChatStorage.Message{DeviceID: deviceID}
			if err := rows.Scan(&rowID, &message.ID, &message.ChatJID, &ts, &message.IsFromMe, &message.Sender); err != nil {
				rows.Close()
				return nil, err
			}
			if ts.IsZero() {
				rows.Close()
				return nil, fmt.Errorf("invalid message timestamp for %s", message.ID)
			}
			message.Timestamp = ts
			// Same second: prefer the later stored row, so the choice is stable.
			if latest == nil || ts.Unix() > latest.Timestamp.Unix() || (ts.Unix() == latest.Timestamp.Unix() && rowID > latestRow) {
				latest, latestRow = message, rowID
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return latest, nil
}
