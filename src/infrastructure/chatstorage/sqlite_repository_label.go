package chatstorage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// sqlExecer is the part of *sql.DB and *sql.Tx the label writes need.
type sqlExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func validLabel(label *domainChatStorage.Label) error {
	if label == nil {
		return fmt.Errorf("label is required")
	}
	if strings.TrimSpace(label.DeviceID) == "" || strings.TrimSpace(label.ID) == "" {
		return fmt.Errorf("label requires device_id and label_id")
	}
	return nil
}

func upsertLabel(db sqlExecer, label *domainChatStorage.Label) error {
	updatedAt := label.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	_, err := db.Exec(`
		INSERT INTO labels (device_id, label_id, name, color, type, order_index, is_active, deleted, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, label_id) DO UPDATE SET
			name = excluded.name,
			color = excluded.color,
			type = excluded.type,
			order_index = excluded.order_index,
			is_active = excluded.is_active,
			deleted = excluded.deleted,
			updated_at = excluded.updated_at
	`, label.DeviceID, label.ID, label.Name, label.Color, label.Type, label.OrderIndex, label.Active, label.Deleted, updatedAt.UTC())
	return err
}

// UpsertLabel stores the current state of a label. Deleting a label also
// drops its chats, so an ID reused later starts empty.
func (r *SQLiteRepository) UpsertLabel(label *domainChatStorage.Label) error {
	if err := validLabel(label); err != nil {
		return err
	}
	if !label.Deleted {
		return upsertLabel(r.db, label)
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	if err := upsertLabel(tx, label); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM label_chats WHERE device_id = ? AND label_id = ?`, label.DeviceID, label.ID); err != nil {
		return fmt.Errorf("failed to delete chats of label %s: %w", label.ID, err)
	}
	return tx.Commit()
}

func scanLabel(scanner interface{ Scan(dest ...any) error }) (*domainChatStorage.Label, error) {
	var label domainChatStorage.Label
	if err := scanner.Scan(&label.DeviceID, &label.ID, &label.Name, &label.Color, &label.Type,
		&label.OrderIndex, &label.Active, &label.Deleted, &label.UpdatedAt); err != nil {
		return nil, err
	}
	return &label, nil
}

// GetLabel returns one label of a device, or nil when it is unknown.
func (r *SQLiteRepository) GetLabel(deviceID, labelID string) (*domainChatStorage.Label, error) {
	label, err := scanLabel(r.db.QueryRow(`
		SELECT device_id, label_id, name, color, type, order_index, is_active, deleted, updated_at
		FROM labels WHERE device_id = ? AND label_id = ?
	`, deviceID, labelID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return label, err
}

// GetLabels returns every label of a device, deleted ones included.
func (r *SQLiteRepository) GetLabels(deviceID string) ([]*domainChatStorage.Label, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, fmt.Errorf("device_id is required for label queries")
	}
	rows, err := r.db.Query(`
		SELECT device_id, label_id, name, color, type, order_index, is_active, deleted, updated_at
		FROM labels WHERE device_id = ? ORDER BY order_index, label_id
	`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []*domainChatStorage.Label
	for rows.Next() {
		label, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

func setLabelChat(db sqlExecer, deviceID, labelID, chatJID string, labeled bool, at time.Time) error {
	if !labeled {
		_, err := db.Exec(`DELETE FROM label_chats WHERE device_id = ? AND label_id = ? AND chat_jid = ?`, deviceID, labelID, chatJID)
		return err
	}
	if at.IsZero() {
		at = time.Now()
	}
	_, err := db.Exec(`
		INSERT INTO label_chats (device_id, label_id, chat_jid, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(device_id, label_id, chat_jid) DO UPDATE SET updated_at = excluded.updated_at
	`, deviceID, labelID, chatJID, at.UTC())
	return err
}

// SetLabelChat adds a chat to a label, or removes it when labeled is false.
func (r *SQLiteRepository) SetLabelChat(deviceID, labelID, chatJID string, labeled bool, at time.Time) error {
	if strings.TrimSpace(deviceID) == "" || strings.TrimSpace(labelID) == "" || strings.TrimSpace(chatJID) == "" {
		return fmt.Errorf("label chat requires device_id, label_id, and chat_jid")
	}
	return setLabelChat(r.db, deviceID, labelID, chatJID, labeled, at)
}

// GetLabelChats returns every chat linked to a label of a device.
func (r *SQLiteRepository) GetLabelChats(deviceID string) ([]*domainChatStorage.LabelChat, error) {
	if strings.TrimSpace(deviceID) == "" {
		return nil, fmt.Errorf("device_id is required for label queries")
	}
	rows, err := r.db.Query(`
		SELECT device_id, label_id, chat_jid, updated_at FROM label_chats
		WHERE device_id = ? ORDER BY label_id, chat_jid
	`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []*domainChatStorage.LabelChat
	for rows.Next() {
		var chat domainChatStorage.LabelChat
		if err := rows.Scan(&chat.DeviceID, &chat.LabelID, &chat.ChatJID, &chat.UpdatedAt); err != nil {
			return nil, err
		}
		chats = append(chats, &chat)
	}
	return chats, rows.Err()
}

// ReplaceLabels swaps every label and label link of a device in one
// transaction. Rows of other devices are not touched.
func (r *SQLiteRepository) ReplaceLabels(deviceID string, labels []*domainChatStorage.Label, chats []*domainChatStorage.LabelChat) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("device id is required")
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM label_chats WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("failed to clear label chats: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM labels WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("failed to clear labels: %w", err)
	}
	for _, label := range labels {
		if label == nil {
			continue
		}
		row := *label
		row.DeviceID = deviceID
		if err := validLabel(&row); err != nil {
			return err
		}
		if err := upsertLabel(tx, &row); err != nil {
			return fmt.Errorf("failed to store label %s: %w", row.ID, err)
		}
	}
	for _, chat := range chats {
		if chat == nil || chat.LabelID == "" || chat.ChatJID == "" {
			continue
		}
		if err := setLabelChat(tx, deviceID, chat.LabelID, chat.ChatJID, true, chat.UpdatedAt); err != nil {
			return fmt.Errorf("failed to store label chat %s/%s: %w", chat.LabelID, chat.ChatJID, err)
		}
	}
	return tx.Commit()
}
