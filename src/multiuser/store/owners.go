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

// ReserveOwner records userID as deviceID's owner only while the user owns
// fewer than max devices. The count and the insert are one SQL statement, and
// SQLite takes the write lock before the statement reads, so concurrent calls
// can never take a user past max. ok is false when the limit is reached.
func (s *Store) ReserveOwner(ctx context.Context, deviceID, userID string, max int) (ok bool, err error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO device_owners (device_id, user_id, created_at)
		 SELECT ?, ?, ? WHERE (SELECT COUNT(*) FROM device_owners WHERE user_id = ?) < ?`,
		deviceID, userID, s.now(), userID, max)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
