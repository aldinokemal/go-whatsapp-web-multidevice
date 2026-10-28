package companion

import (
	"database/sql"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// State shares the original controller's schema and flock names. Never copy or reset
// this database to retry an uncertain attempt.
type State struct {
	db        *sql.DB
	directory string
}
type Phase struct {
	Phase   string  `json:"phase,omitempty"`
	Reason  string  `json:"reason,omitempty"`
	RetryAt float64 `json:"retry_at,omitempty"`
}

func privateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	s, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !s.IsDir() || s.Mode().Perm()&0077 != 0 {
		return block("invalid_request")
	}
	return nil
}
func OpenState(directory string) (*State, error) {
	if e := privateDir(directory); e != nil {
		return nil, e
	}
	path := filepath.Join(directory, "recovery.sqlite3")
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return nil, block("invalid_request")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: path}
	db, e := sql.Open(sqlite.DriverName, u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	// FULL sync is intentional: a pairing intent must survive a process/host crash.
	for _, q := range []string{"PRAGMA busy_timeout=5000", "PRAGMA synchronous=FULL", "CREATE TABLE IF NOT EXISTS recovery (account TEXT, backend TEXT, phase TEXT NOT NULL, reason TEXT NOT NULL, retry_at REAL NOT NULL, updated_at REAL NOT NULL, PRIMARY KEY(account,backend))"} {
		if _, e = db.Exec(q); e != nil {
			db.Close()
			return nil, e
		}
	}
	return &State{db: db, directory: directory}, nil
}
func (s *State) Close() error { return s.db.Close() }
func (s *State) Get(account, backend string) (Phase, error) {
	var p Phase
	e := s.db.QueryRow("SELECT phase,reason,retry_at FROM recovery WHERE account=? AND backend=?", account, backend).Scan(&p.Phase, &p.Reason, &p.RetryAt)
	if e == sql.ErrNoRows {
		e = nil
	}
	return p, e
}
func (s *State) Put(account, backend, phase, reason string, retryAt float64) error {
	_, e := s.db.Exec("INSERT INTO recovery VALUES (?,?,?,?,?,?) ON CONFLICT(account,backend) DO UPDATE SET phase=excluded.phase,reason=excluded.reason,retry_at=excluded.retry_at,updated_at=excluded.updated_at", account, backend, phase, reason, retryAt, float64(time.Now().UnixNano())/1e9)
	return e
}
