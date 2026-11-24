package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Refusal reasons recorded against a slot when its link was refused.
const (
	RefusalDuplicate  = "duplicate"  // the number is linked to another account
	RefusalUnreadable = "unreadable" // the number could not be read or checked
)

// Slot is what accounts.db remembers about one device slot beyond its owner:
// the last number and name it held, whether the phone signed it out, and why
// its last link was refused. It survives the WhatsApp session being cleared.
type Slot struct {
	DeviceID    string
	LastPhone   string
	LastName    string
	SignedOutAt *time.Time
	Refusal     string
	RefusalAt   *time.Time
}

func nullTimePtr(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	t := n.Time
	return &t
}

// SlotsOf returns the user's slots keyed by device id.
func (s *Store) SlotsOf(ctx context.Context, userID string) (map[string]Slot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id, last_phone, last_name, signed_out_at, last_refusal, last_refusal_at
		 FROM device_owners WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Slot{}
	for rows.Next() {
		var sl Slot
		var out1, ref sql.NullTime
		if err := rows.Scan(&sl.DeviceID, &sl.LastPhone, &sl.LastName, &out1, &sl.Refusal, &ref); err != nil {
			return nil, err
		}
		sl.SignedOutAt, sl.RefusalAt = nullTimePtr(out1), nullTimePtr(ref)
		out[sl.DeviceID] = sl
	}
	return out, rows.Err()
}

// RecordIdentity stores the number and name a connected slot holds and clears
// its signed-out mark. Empty values keep what was known. It leaves a refusal
// alone: only an accepted link (ClearRefusal) ends that.
func (s *Store) RecordIdentity(ctx context.Context, deviceID, phone, name string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE device_owners SET
			last_phone = CASE WHEN ? = '' THEN last_phone ELSE ? END,
			last_name = CASE WHEN ? = '' THEN last_name ELSE ? END,
			signed_out_at = NULL
		 WHERE device_id = ?`, phone, phone, name, name, deviceID)
	return err
}

// MarkSignedOut records that the phone signed this slot out, keeping the
// number and name it held (empty values keep what was known).
func (s *Store) MarkSignedOut(ctx context.Context, deviceID, phone, name string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE device_owners SET
			last_phone = CASE WHEN ? = '' THEN last_phone ELSE ? END,
			last_name = CASE WHEN ? = '' THEN last_name ELSE ? END,
			signed_out_at = ?
		 WHERE device_id = ?`, phone, phone, name, name, s.now(), deviceID)
	return err
}

// RecordRefusal records why the slot's latest link was refused.
func (s *Store) RecordRefusal(ctx context.Context, deviceID, reason string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE device_owners SET last_refusal = ?, last_refusal_at = ? WHERE device_id = ?`, reason, s.now(), deviceID)
	return err
}

// ClearRefusal forgets a refusal once a link has been accepted.
func (s *Store) ClearRefusal(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE device_owners SET last_refusal = '', last_refusal_at = NULL WHERE device_id = ?`, deviceID)
	return err
}

// SlotOf returns one slot by device id.
func (s *Store) SlotOf(ctx context.Context, deviceID string) (Slot, error) {
	var sl Slot
	var out1, ref sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT device_id, last_phone, last_name, signed_out_at, last_refusal, last_refusal_at
		 FROM device_owners WHERE device_id = ?`, deviceID).Scan(&sl.DeviceID, &sl.LastPhone, &sl.LastName, &out1, &sl.Refusal, &ref)
	if errors.Is(err, sql.ErrNoRows) {
		return Slot{}, ErrNotFound
	}
	if err != nil {
		return Slot{}, err
	}
	sl.SignedOutAt, sl.RefusalAt = nullTimePtr(out1), nullTimePtr(ref)
	return sl, nil
}
