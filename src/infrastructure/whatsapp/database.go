package whatsapp

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
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
		// applyPostgresTLSDefault preserves an explicit sslmode. Otherwise
		// loopback hosts use sslmode=disable and every other host uses
		// sslmode=verify-full. Set sslmode in DB_URI or DB_KEYS_URI to override.
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
// contain an sslmode query parameter.
//
// An explicit sslmode is preserved, including disable, require, verify-ca,
// and verify-full. Override the defaults by setting sslmode in DB_URI or
// DB_KEYS_URI.
//
// Loopback hosts (localhost, 127.0.0.1, and ::1) and an empty host, which
// lib/pq treats as localhost, default to sslmode=disable so a local Postgres
// that does not speak TLS still accepts the connection. lib/pq's implicit
// default requires TLS and does not verify the server certificate, which
// rejects that local server. Every other host defaults to sslmode=verify-full,
// which checks the certificate against the system trust store and the
// requested hostname. This package has no separate CA bundle, so verify-ca
// is not used.
func applyPostgresTLSDefault(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse postgres dsn: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("unsupported postgres scheme %q", u.Scheme)
	}
	if u.Query().Has("sslmode") {
		return dsn, nil
	}
	mode := "verify-full"
	if isLocalPostgresHost(u.Hostname()) {
		mode = "disable"
	}
	return appendDSNQuery(dsn, "sslmode="+mode), nil
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

func appendDSNQuery(dsn, pair string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	if strings.HasSuffix(dsn, "?") || strings.HasSuffix(dsn, "&") {
		sep = ""
	}
	return dsn + sep + pair
}
