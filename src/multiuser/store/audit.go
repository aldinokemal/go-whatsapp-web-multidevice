package store

import "context"

type AuditEvent struct {
	UserID   string
	Action   string
	DeviceID string
	IP       string
}

func (s *Store) Audit(ctx context.Context, e AuditEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (user_id, action, device_id, ip, at) VALUES (?, ?, ?, ?, ?)`,
		e.UserID, e.Action, e.DeviceID, e.IP, s.Now())
	return err
}
