package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) SetOwner(ctx context.Context, deviceID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO device_owners (device_id, user_id, created_at) VALUES (?, ?, ?)`, deviceID, userID, s.now())
	return err
}

func (s *Store) OwnerOf(ctx context.Context, deviceID string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM device_owners WHERE device_id = ?`, deviceID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return userID, err
}

func (s *Store) DevicesOf(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id FROM device_owners WHERE user_id = ? ORDER BY created_at, device_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) RemoveOwner(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM device_owners WHERE device_id = ?`, deviceID)
	return err
}

func (s *Store) OwnedDeviceCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_owners`).Scan(&n)
	return n, err
}

// Reservation is ReserveOwner's answer.
type Reservation int

const (
	Reserved   Reservation = iota + 1 // the slot is the user's
	UserLimit                         // the user already has maxPerUser numbers
	ServerFull                        // the server already has maxTotal numbers
)

// ReserveOwner records userID as deviceID's owner only while the user owns
// fewer than maxPerUser devices and, when maxTotal > 0, the server holds
// fewer than maxTotal. Both counts and the insert are one SQL statement, and
// SQLite takes the write lock before the statement reads, so concurrent calls
// can never take a user past their limit or the server past its cap. Every
// owner row counts, including a slot whose QR code was never scanned: it
// holds a reservation.
//
// When nothing was inserted, a second read names the limit that was hit, for
// the message only; the decision was already made atomically. When both
// were hit, the user's own limit is named.
func (s *Store) ReserveOwner(ctx context.Context, deviceID, userID string, maxPerUser, maxTotal int) (Reservation, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO device_owners (device_id, user_id, created_at)
		 SELECT ?, ?, ?
		 WHERE (SELECT COUNT(*) FROM device_owners WHERE user_id = ?) < ?
		   AND (? = 0 OR (SELECT COUNT(*) FROM device_owners) < ?)`,
		deviceID, userID, s.now(), userID, maxPerUser, maxTotal, maxTotal)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		return Reserved, nil
	}
	var mine int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_owners WHERE user_id = ?`, userID).Scan(&mine); err != nil {
		return 0, err
	}
	if mine >= maxPerUser {
		return UserLimit, nil
	}
	return ServerFull, nil
}
