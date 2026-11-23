package store

import (
	"context"
	"time"
)

// AuditEvent is one line of the audit log. UserID is the account the event is
// about; ActorID is who did it when that is someone else (an admin). Detail is
// a short, non-secret note (an old and new number, a limit). Never put a
// token, session id or secret in any field.
type AuditEvent struct {
	UserID   string
	ActorID  string
	Action   string
	DeviceID string
	IP       string
	Detail   string
}

func (s *Store) Audit(ctx context.Context, e AuditEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (user_id, actor_id, action, device_id, ip, detail, at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.UserID, e.ActorID, e.Action, e.DeviceID, e.IP, e.Detail, s.now())
	return err
}

// AuditRow is a stored audit event.
type AuditRow struct {
	ID int64
	AuditEvent
	At time.Time
}

// AuditQuery pages the audit log. UserID, when set, keeps events about that
// user or done by them.
type AuditQuery struct {
	UserID string
	Limit  int
	Offset int
}

// ListAudit returns one page of the audit log, newest first, and how many
// events matched in all.
func (s *Store) ListAudit(ctx context.Context, q AuditQuery) ([]AuditRow, int, error) {
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	where, args := "", []any{}
	if q.UserID != "" {
		where = ` WHERE user_id = ? OR actor_id = ?`
		args = append(args, q.UserID, q.UserID)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, actor_id, action, device_id, ip, detail, at FROM audit_log`+where+
			` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []AuditRow
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.ID, &r.UserID, &r.ActorID, &r.Action, &r.DeviceID, &r.IP, &r.Detail, &r.At); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}
