package chatstorage

import (
	"database/sql"

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
			file_enc_sha256, file_length, referral_metadata, created_at, updated_at
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
func (r *SQLiteRepository) GetOldestMessageByDevice(deviceID, chatJID string) (*domainChatStorage.Message, error) {
	query := `
		SELECT id, chat_jid, device_id, sender, content, timestamp, is_from_me,
			media_type, call_metadata, filename, url, direct_path, media_key, file_sha256,
			file_enc_sha256, file_length, referral_metadata, created_at, updated_at
		FROM messages
		WHERE chat_jid = ? AND device_id = ?
		ORDER BY timestamp ASC
		LIMIT 1
	`

	message, err := r.scanMessage(r.db.QueryRow(query, chatJID, deviceID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return message, err
}
