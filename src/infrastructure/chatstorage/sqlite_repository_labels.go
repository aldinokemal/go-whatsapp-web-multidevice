package chatstorage

import (
	"fmt"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
)

// UpsertLabel stores a label as last set by app state. A set replaces the
// whole label value, so every column is overwritten.
func (r *SQLiteRepository) UpsertLabel(label *domainChatStorage.Label) error {
	if label == nil || strings.TrimSpace(label.DeviceID) == "" || strings.TrimSpace(label.LabelID) == "" {
		return fmt.Errorf("label requires device_id and label_id")
	}
	_, err := r.db.Exec(`
		INSERT INTO labels (
			device_id, label_id, name, color, predefined_id, type,
			order_index, is_active, is_immutable, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(device_id, label_id) DO UPDATE SET
			name = excluded.name,
			color = excluded.color,
			predefined_id = excluded.predefined_id,
			type = excluded.type,
			order_index = excluded.order_index,
			is_active = excluded.is_active,
			is_immutable = excluded.is_immutable,
			updated_at = excluded.updated_at
	`, label.DeviceID, label.LabelID, label.Name, label.Color, label.PredefinedID, label.Type,
		label.OrderIndex, label.IsActive, label.IsImmutable, time.Now().UTC())
	return err
}

// DeleteLabel removes a label and its chat associations.
func (r *SQLiteRepository) DeleteLabel(deviceID, labelID string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM label_chats WHERE device_id = ? AND label_id = ?`, deviceID, labelID); err != nil {
		return fmt.Errorf("failed to delete label chats: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM labels WHERE device_id = ? AND label_id = ?`, deviceID, labelID); err != nil {
		return fmt.Errorf("failed to delete label: %w", err)
	}
	return tx.Commit()
}

// UpsertLabelAssociation records that a chat carries a label.
func (r *SQLiteRepository) UpsertLabelAssociation(association *domainChatStorage.LabelAssociation) error {
	if association == nil || strings.TrimSpace(association.DeviceID) == "" ||
		strings.TrimSpace(association.LabelID) == "" || strings.TrimSpace(association.ChatJID) == "" {
		return fmt.Errorf("label association requires device_id, label_id, and chat_jid")
	}
	_, err := r.db.Exec(`
		INSERT INTO label_chats (device_id, label_id, chat_jid, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(device_id, label_id, chat_jid) DO UPDATE SET updated_at = excluded.updated_at
	`, association.DeviceID, association.LabelID, association.ChatJID, time.Now().UTC())
	return err
}

// DeleteLabelAssociation removes a chat from a label.
func (r *SQLiteRepository) DeleteLabelAssociation(deviceID, labelID, chatJID string) error {
	_, err := r.db.Exec(`DELETE FROM label_chats WHERE device_id = ? AND label_id = ? AND chat_jid = ?`,
		deviceID, labelID, chatJID)
	return err
}

// ListLabels returns the device's labels ordered by order index, then ID.
func (r *SQLiteRepository) ListLabels(deviceID string) ([]*domainChatStorage.Label, error) {
	rows, err := r.db.Query(`
		SELECT device_id, label_id, name, color, predefined_id, type,
			order_index, is_active, is_immutable, updated_at
		FROM labels
		WHERE device_id = ?
		ORDER BY order_index, label_id
	`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var labels []*domainChatStorage.Label
	for rows.Next() {
		label := &domainChatStorage.Label{}
		if err := rows.Scan(&label.DeviceID, &label.LabelID, &label.Name, &label.Color, &label.PredefinedID, &label.Type,
			&label.OrderIndex, &label.IsActive, &label.IsImmutable, &label.UpdatedAt); err != nil {
			return nil, err
		}
		labels = append(labels, label)
	}
	return labels, rows.Err()
}

// ListLabelAssociations returns the associations of labels that are stored,
// so chats of a deleted or unknown label are left out.
func (r *SQLiteRepository) ListLabelAssociations(deviceID, labelID string) ([]*domainChatStorage.LabelAssociation, error) {
	query := `
		SELECT lc.device_id, lc.label_id, lc.chat_jid, lc.updated_at
		FROM label_chats lc
		JOIN labels l ON l.device_id = lc.device_id AND l.label_id = lc.label_id
		WHERE lc.device_id = ?`
	args := []any{deviceID}
	if labelID != "" {
		query += ` AND lc.label_id = ?`
		args = append(args, labelID)
	}
	query += ` ORDER BY lc.label_id, lc.chat_jid`

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var associations []*domainChatStorage.LabelAssociation
	for rows.Next() {
		association := &domainChatStorage.LabelAssociation{}
		if err := rows.Scan(&association.DeviceID, &association.LabelID, &association.ChatJID, &association.UpdatedAt); err != nil {
			return nil, err
		}
		associations = append(associations, association)
	}
	return associations, rows.Err()
}

// DeleteLabelDataBefore removes the device's labels and associations that
// were last written before the cutoff. Rows are written with UTC times, so
// the text comparison holds in both SQLite drivers' time formats.
func (r *SQLiteRepository) DeleteLabelDataBefore(deviceID string, before time.Time) error {
	if strings.TrimSpace(deviceID) == "" {
		return fmt.Errorf("device id is required")
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	cutoff := before.UTC()
	if _, err := tx.Exec(`DELETE FROM label_chats WHERE device_id = ? AND updated_at < ?`, deviceID, cutoff); err != nil {
		return fmt.Errorf("failed to delete stale label chats: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM labels WHERE device_id = ? AND updated_at < ?`, deviceID, cutoff); err != nil {
		return fmt.Errorf("failed to delete stale labels: %w", err)
	}
	return tx.Commit()
}
