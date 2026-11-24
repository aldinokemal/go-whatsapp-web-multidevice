package store

import (
	"context"
	"time"
)

// neverLinked is the condition for a slot that was reserved and never held a
// WhatsApp identity: no number or name was ever recorded, the phone never
// signed it out, and no link to it was refused (a refusal means a phone did
// scan it). A slot signed out from the phone is not one of these and is kept.
const neverLinked = `last_phone = '' AND last_name = '' AND signed_out_at IS NULL AND last_refusal = ''`

// OwnedSlot is a slot and the user who owns it.
type OwnedSlot struct {
	DeviceID string
	UserID   string
}

// NeverLinkedSlotsBefore lists slots reserved before cutoff that never held a
// WhatsApp identity, oldest first. cutoff may be in any zone: it is compared
// as UTC text against created_at, which the store always writes in UTC.
func (s *Store) NeverLinkedSlotsBefore(ctx context.Context, cutoff time.Time) ([]OwnedSlot, error) {
	cutoff = cutoff.UTC()
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id, user_id FROM device_owners WHERE `+neverLinked+` AND created_at < ?
		 ORDER BY created_at, device_id`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OwnedSlot
	for rows.Next() {
		var sl OwnedSlot
		if err := rows.Scan(&sl.DeviceID, &sl.UserID); err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}

// ExpireNeverLinkedSlot deletes deviceID's owner row only if it is still a
// never-linked slot reserved before cutoff, in one statement, so a slot that
// was scanned in the meantime is never taken. It reports whether it did.
func (s *Store) ExpireNeverLinkedSlot(ctx context.Context, deviceID string, cutoff time.Time) (bool, error) {
	cutoff = cutoff.UTC() // see NeverLinkedSlotsBefore
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM device_owners WHERE device_id = ? AND `+neverLinked+` AND created_at < ?`, deviceID, cutoff)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
