package whatsapp

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/lib/pq"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// InitWaDB initializes the WhatsApp database connection
func InitWaDB(ctx context.Context, DBURI string) *sqlstore.Container {
	log = waLog.Stdout("Main", config.WhatsappLogLevel, true)
	dbLog := waLog.Stdout("Database", config.WhatsappLogLevel, true)

	storeContainer, err := initDatabase(ctx, dbLog, DBURI)
	if err != nil {
		log.Errorf("Database initialization error: %v", err)
		panic(pkgError.InternalServerError(fmt.Sprintf("Database initialization error: %v", err)))
	}

	return storeContainer
}

// initDatabase creates and returns a database store container based on the configured URI
func initDatabase(ctx context.Context, dbLog waLog.Logger, DBURI string) (*sqlstore.Container, error) {
	// Strip surrounding quotes that may come from .env file parsing
	DBURI = strings.Trim(DBURI, `"'`)

	if strings.HasPrefix(DBURI, "file:") {
		// Restrict the session file before the driver creates it. Both the CGO
		// sqlite3 driver and modernc.org/sqlite (-tags purego) honor an
		// existing file's mode, and a 0700 directory covers sidecars created
		// during open. FormatChatStorageURI still applies WAL, the 30s busy
		// timeout, and foreign keys.
		if err := secureSQLiteSessionFile(DBURI); err != nil {
			return nil, fmt.Errorf("prepare sqlite session file: %w", err)
		}
		DBURI = sqlite.FormatChatStorageURI(DBURI, true, true)
		container, err := sqlstore.New(ctx, sqlite.DriverName, DBURI, dbLog)
		if err != nil {
			return nil, err
		}
		if err := secureSQLiteSessionFile(DBURI); err != nil {
			_ = container.Close()
			return nil, fmt.Errorf("restrict sqlite session file: %w", err)
		}
		return container, nil
	} else if strings.HasPrefix(DBURI, "postgres:") {
		// applyPostgresTLSDefault preserves a non-empty sslmode. Otherwise the
		// host lib/pq will dial selects sslmode=disable or sslmode=verify-full.
		// Set sslmode in DB_URI or DB_KEYS_URI to override.
		secured, err := applyPostgresTLSDefault(DBURI)
		if err != nil {
			return nil, err
		}
		return sqlstore.New(ctx, "postgres", secured, dbLog)
	}

	return nil, fmt.Errorf("unknown database type: %s. Currently only sqlite3(file:) and postgres are supported", DBURI)
}

// secureSQLiteSessionFile makes the session directory owner-only (0700) and the
// database file owner-read/write (0600). Memory and other non-file URIs are
// left untouched. Existing files are tightened in place; they are not truncated.
func secureSQLiteSessionFile(uri string) error {
	target, err := sqliteSessionTarget(uri)
	if err != nil {
		return err
	}
	if target.skip {
		return nil
	}

	dir := filepath.Dir(target.path)
	if err := secureSQLiteSessionDir(dir); err != nil {
		return err
	}
	if err := secureSQLiteSessionDB(target.path, target.create); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := chmodExistingFile(target.path+suffix, 0600); err != nil {
			return err
		}
	}
	return nil
}

type sqliteTarget struct {
	path   string
	create bool
	skip   bool
}

func sqliteSessionTarget(uri string) (sqliteTarget, error) {
	raw := strings.TrimPrefix(uri, "file:")
	base, query, _ := strings.Cut(raw, "?")
	create := true
	if query != "" {
		q, err := url.ParseQuery(query)
		if err != nil {
			return sqliteTarget{}, fmt.Errorf("parse sqlite uri: %w", err)
		}
		if strings.EqualFold(q.Get("mode"), "memory") {
			return sqliteTarget{skip: true}, nil
		}
		mode := strings.ToLower(q.Get("mode"))
		if mode == "ro" || q.Get("immutable") == "1" {
			create = false
		}
	}
	if base == "" || base == ":memory:" || strings.HasPrefix(base, ":memory:") {
		return sqliteTarget{skip: true}, nil
	}

	path, err := sqliteURIPath(base)
	if err != nil {
		return sqliteTarget{}, err
	}
	if path == "" || path == "." || path == string(filepath.Separator) {
		return sqliteTarget{skip: true}, nil
	}
	return sqliteTarget{path: path, create: create}, nil
}

func sqliteURIPath(base string) (string, error) {
	var path string
	if strings.HasPrefix(base, "//") {
		u, err := url.Parse("file:" + base)
		if err != nil {
			return "", fmt.Errorf("parse sqlite uri: %w", err)
		}
		path = u.Path
	} else {
		decoded, err := url.PathUnescape(base)
		if err != nil {
			return "", fmt.Errorf("parse sqlite uri: %w", err)
		}
		path = decoded
	}
	if path == "" {
		return "", nil
	}
	return filepath.Clean(path), nil
}

func secureSQLiteSessionDir(dir string) error {
	if dir == "" || dir == "." || filepath.Clean(dir) == string(filepath.Separator) {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.Chmod(dir, 0700)
}

func secureSQLiteSessionDB(path string, create bool) error {
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if !create {
			return nil
		}
		// OpenFile applies the process umask, so a follow-up Chmod is required
		// to land on 0600 even when umask is 0.
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return os.Chmod(path, 0600)
}

func chmodExistingFile(path string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("sqlite session path %s is a directory", path)
	}
	return os.Chmod(path, mode)
}

// applyPostgresTLSDefault sets a TLS mode when the DSN does not already
// contain a non-empty sslmode query parameter.
//
// lib/pq reads only the first sslmode value (url.Values.Get) and drops it
// when that value is blank, which leaves SSLMode empty and skips certificate
// checks. A non-empty first value is preserved, including disable, require,
// verify-ca, and verify-full. A missing or blank first value is not a choice.
// Later blank entries are dropped; if a later value is non-empty, that single
// value is written so pq.NewConfig reports it. Otherwise the dial-target
// default applies. Override the defaults with a non-empty first sslmode in
// DB_URI or DB_KEYS_URI.
//
// The mode follows the address lib/pq will dial, from pq.NewConfig: URL
// authority, query host and hostaddr, PGHOST, and PGHOSTADDR. If any dial
// target is not loopback (localhost, 127.0.0.1, ::1) and not a unix-socket
// path, the default is sslmode=verify-full. Loopback and unix-socket targets
// use sslmode=disable so a local Postgres without TLS still connects.
// PGSSLMODE=verify-full is left in place when the DSN does not need a
// replacement value. The query is written on the parsed URL so a fragment
// stays a fragment.
func applyPostgresTLSDefault(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse postgres dsn: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("unsupported postgres scheme %q", u.Scheme)
	}
	if _, ok := leadingPostgresSSLMode(u); ok {
		return dsn, nil
	}
	if promoted := promotedPostgresSSLMode(u); promoted != "" {
		return setPostgresSSLMode(u, promoted), nil
	}

	remote, err := postgresDialTargetIsRemote(dsn)
	if err != nil {
		return "", err
	}
	// PGSSLMODE=verify-full already verifies the server. lib/pq drops a blank
	// sslmode, so that environment value still applies. Leave the DSN unchanged
	// instead of writing sslmode=disable over it.
	if strings.TrimSpace(os.Getenv("PGSSLMODE")) == "verify-full" {
		return dsn, nil
	}
	mode := "disable"
	if remote {
		mode = "verify-full"
	}
	return setPostgresSSLMode(u, mode), nil
}

// leadingPostgresSSLMode reports the first sslmode query value when it is
// non-empty. That is the only value lib/pq will read.
func leadingPostgresSSLMode(u *url.URL) (string, bool) {
	values, ok := u.Query()["sslmode"]
	if !ok || len(values) == 0 {
		return "", false
	}
	trimmed := strings.TrimSpace(values[0])
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

// promotedPostgresSSLMode returns the first non-blank sslmode after a missing
// or blank first entry. lib/pq never sees those later entries.
func promotedPostgresSSLMode(u *url.URL) string {
	for _, value := range u.Query()["sslmode"] {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// postgresDialTargetIsRemote reports whether lib/pq will dial any TCP address
// that is not the loopback names this package treats as local.
func postgresDialTargetIsRemote(dsn string) (bool, error) {
	cfg, err := pq.NewConfig(dsn)
	if err != nil {
		return false, fmt.Errorf("parse postgres dial target: %w", err)
	}
	if postgresTargetIsRemote(cfg.Host, cfg.Hostaddr) {
		return true, nil
	}
	for _, extra := range cfg.Multi {
		if postgresTargetIsRemote(extra.Host, extra.Hostaddr) {
			return true, nil
		}
	}
	return false, nil
}

func postgresTargetIsRemote(host string, addr netip.Addr) bool {
	if addr.IsValid() {
		return !isPostgresLoopbackIP(addr)
	}
	host = strings.TrimSpace(host)
	if host == "" || filepath.IsAbs(host) || strings.HasPrefix(host, "@") {
		return false
	}
	return !isLocalPostgresHost(host)
}

func isPostgresLoopbackIP(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	if addr.Is4() {
		return addr == netip.MustParseAddr("127.0.0.1")
	}
	return addr == netip.MustParseAddr("::1")
}

func isLocalPostgresHost(host string) bool {
	host = strings.TrimSpace(host)
	host = strings.Trim(host, "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.Equal(net.ParseIP("127.0.0.1")) || ip.Equal(net.IPv6loopback)
}

func setPostgresSSLMode(u *url.URL, mode string) string {
	cloned := *u
	query := cloned.Query()
	query.Set("sslmode", mode)
	cloned.RawQuery = query.Encode()
	return cloned.String()
}
