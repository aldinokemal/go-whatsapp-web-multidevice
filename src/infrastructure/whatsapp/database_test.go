package whatsapp

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/lib/pq"
)

func TestApplyPostgresTLSDefault(t *testing.T) {
	isolatePostgresDialEnv(t)

	tests := []struct {
		name      string
		dsn       string
		pgsslmode string
		wantMode  string
		same      bool
	}{
		{
			name:     "public host gains verify-full",
			dsn:      "postgres://user:secret@db.example.com:5432/whatsapp",
			wantMode: "verify-full",
		},
		{
			name:     "public host keeps existing query and encoded password",
			dsn:      "postgres://user:p%40ss@db.example.com/whatsapp?connect_timeout=5",
			wantMode: "verify-full",
		},
		{
			name:     "localhost is not forced to verify-full",
			dsn:      "postgres://user:secret@localhost:5432/whatsapp",
			wantMode: "disable",
		},
		{
			name:     "localhost hostname is case-insensitive",
			dsn:      "postgresql://user:secret@LocalHost/whatsapp",
			wantMode: "disable",
		},
		{
			name:     "ipv4 loopback is not forced to verify-full",
			dsn:      "postgres://user:secret@127.0.0.1:5432/whatsapp",
			wantMode: "disable",
		},
		{
			name:     "ipv6 loopback is not forced to verify-full",
			dsn:      "postgres://user:secret@[::1]:5432/whatsapp",
			wantMode: "disable",
		},
		{
			name:     "empty host follows lib/pq localhost default",
			dsn:      "postgres://user:secret@/whatsapp",
			wantMode: "disable",
		},
		{
			name: "explicit sslmode on a public host is preserved",
			dsn:  "postgres://user:secret@db.example.com/whatsapp?sslmode=require",
			same: true,
		},
		{
			name: "explicit disable on a public host is preserved",
			dsn:  "postgres://user:secret@db.example.com/whatsapp?application_name=gowa&sslmode=disable",
			same: true,
		},
		{
			name: "explicit verify-full on localhost is preserved",
			dsn:  "postgres://user:secret@localhost/whatsapp?sslmode=verify-full",
			same: true,
		},
		{
			name:     "localhost suffix is not treated as loopback",
			dsn:      "postgres://user:secret@localhost.example.com/whatsapp",
			wantMode: "verify-full",
		},
		{
			name:     "localhost.evil.com stays verify-full",
			dsn:      "postgres://user:secret@localhost.evil.com/whatsapp",
			wantMode: "verify-full",
		},
		{
			name:     "userinfo is not the host",
			dsn:      "postgres://localhost:secret@db.example.com/whatsapp",
			wantMode: "verify-full",
		},
		{
			name:      "PGSSLMODE=require on localhost is not replaced with disable",
			dsn:       "postgres://user:secret@localhost/whatsapp",
			pgsslmode: "require",
			same:      true,
		},
		{
			name:      "PGSSLMODE=disable on a public host is not replaced with verify-full",
			dsn:       "postgres://user:secret@db.example.com/whatsapp",
			pgsslmode: "disable",
			same:      true,
		},
		{
			name:      "PGSSLMODE=verify-full on localhost is not replaced with disable",
			dsn:       "postgres://user:secret@localhost/whatsapp",
			pgsslmode: "verify-full",
			same:      true,
		},
		{
			name: "sslrootcert=system on localhost keeps its implied verify-full",
			dsn:  "postgres://user:secret@localhost/whatsapp?sslrootcert=system",
			same: true,
		},
		{
			name: "sslnegotiation=direct on localhost is not given disable",
			dsn:  "postgres://user:secret@localhost/whatsapp?sslnegotiation=direct",
			same: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pgsslmode != "" {
				t.Setenv("PGSSLMODE", tc.pgsslmode)
			}
			got, err := applyPostgresTLSDefault(tc.dsn)
			if err != nil {
				t.Fatalf("applyPostgresTLSDefault: %v", err)
			}
			if tc.same {
				if got != tc.dsn {
					t.Fatalf("explicit sslmode changed\n got %s\nwant %s", got, tc.dsn)
				}
				mustPostgresConfig(t, got)
				return
			}
			sep := "?"
			if strings.Contains(tc.dsn, "?") {
				sep = "&"
			}
			want := tc.dsn + sep + "sslmode=" + tc.wantMode
			if got != want {
				t.Fatalf("dsn = %s, want %s", got, want)
			}
			u, err := url.Parse(got)
			if err != nil {
				t.Fatalf("parse result: %v", err)
			}
			if u.Query().Get("sslmode") != tc.wantMode {
				t.Fatalf("sslmode = %q, want %q", u.Query().Get("sslmode"), tc.wantMode)
			}
			if strings.Contains(tc.dsn, "p%40ss") && !strings.Contains(got, "p%40ss") {
				t.Fatalf("encoded password was rewritten: %s", got)
			}
		})
	}
}

func TestApplyPostgresTLSDefaultUsesDialTarget(t *testing.T) {
	isolatePostgresDialEnv(t)

	t.Run("remote hostaddr without a certificate host name is rejected", func(t *testing.T) {
		// verify-full would check the certificate against localhost or "".
		for _, in := range []string{
			"postgres://user:secret@localhost/whatsapp?hostaddr=203.0.113.10",
			"postgres://user:secret@/whatsapp?hostaddr=203.0.113.10",
		} {
			assertRemoteHostaddrRejected(t, in)
		}
		// An explicit sslmode is the documented way out.
		in := "postgres://user:secret@localhost/whatsapp?hostaddr=203.0.113.10&sslmode=require"
		if got := mustApplyPostgresTLSDefault(t, in); got != in {
			t.Fatalf("explicit sslmode changed\n got %s\nwant %s", got, in)
		}
	})

	t.Run("query host overrides localhost", func(t *testing.T) {
		in := "postgres://user:secret@localhost/whatsapp?host=prod.example.com"
		got := mustApplyPostgresTLSDefault(t, in)
		assertPostgresSSLMode(t, got, "verify-full")
		cfg := mustPostgresConfig(t, got)
		if cfg.Host != "prod.example.com" {
			t.Fatalf("dial host = %q, want prod.example.com", cfg.Host)
		}
		if cfg.SSLMode != pq.SSLModeVerifyFull {
			t.Fatalf("SSLMode = %q, want verify-full", cfg.SSLMode)
		}
	})

	t.Run("PGHOST on empty authority", func(t *testing.T) {
		t.Setenv("PGHOST", "db.example.com")
		in := "postgres://user:secret@/whatsapp"
		got := mustApplyPostgresTLSDefault(t, in)
		if strings.Contains(got, "sslmode=disable") {
			t.Fatalf("remote PGHOST was given sslmode=disable: %s", got)
		}
		assertPostgresSSLMode(t, got, "verify-full")
		cfg := mustPostgresConfig(t, got)
		if cfg.Host != "db.example.com" {
			t.Fatalf("dial host = %q, want db.example.com", cfg.Host)
		}
		if cfg.SSLMode != pq.SSLModeVerifyFull {
			t.Fatalf("SSLMode = %q, want verify-full", cfg.SSLMode)
		}
	})

	t.Run("PGSSLMODE verify-full is not replaced with disable", func(t *testing.T) {
		t.Setenv("PGHOST", "db.example.com")
		t.Setenv("PGSSLMODE", "verify-full")
		in := "postgres://user:secret@/whatsapp"
		got := mustApplyPostgresTLSDefault(t, in)
		if strings.Contains(got, "sslmode=disable") {
			t.Fatalf("PGSSLMODE=verify-full was rewritten to disable: %s", got)
		}
		if got != in {
			t.Fatalf("DSN was rewritten even though PGSSLMODE=verify-full\n got %s\nwant %s", got, in)
		}
		cfg := mustPostgresConfig(t, got)
		if cfg.Host != "db.example.com" {
			t.Fatalf("dial host = %q, want db.example.com", cfg.Host)
		}
		if cfg.SSLMode != pq.SSLModeVerifyFull {
			t.Fatalf("SSLMode = %q, want verify-full", cfg.SSLMode)
		}
	})

	t.Run("remote PGHOSTADDR with localhost host is rejected", func(t *testing.T) {
		t.Setenv("PGHOSTADDR", "203.0.113.10")
		assertRemoteHostaddrRejected(t, "postgres://user:secret@localhost/whatsapp")
	})

	t.Run("blank first sslmode yields to a later verify-full", func(t *testing.T) {
		in := "postgres://user:secret@db.example.com/whatsapp?sslmode=&sslmode=verify-full"
		got := mustApplyPostgresTLSDefault(t, in)
		assertPostgresSSLMode(t, got, "verify-full")
		cfg := mustPostgresConfig(t, got)
		if cfg.Host != "db.example.com" {
			t.Fatalf("dial host = %q, want db.example.com", cfg.Host)
		}
		if cfg.SSLMode != pq.SSLModeVerifyFull {
			t.Fatalf("SSLMode = %q, want verify-full", cfg.SSLMode)
		}
	})

	t.Run("blank first sslmode yields to a later require", func(t *testing.T) {
		in := "postgres://user:secret@db.example.com/whatsapp?sslmode=&sslmode=require"
		got := mustApplyPostgresTLSDefault(t, in)
		assertPostgresSSLMode(t, got, "require")
		cfg := mustPostgresConfig(t, got)
		if cfg.SSLMode != pq.SSLModeRequire {
			t.Fatalf("SSLMode = %q, want require", cfg.SSLMode)
		}
	})

	t.Run("non-empty first sslmode stays ahead of a blank repeat", func(t *testing.T) {
		in := "postgres://user:secret@db.example.com/whatsapp?sslmode=verify-full&sslmode="
		got := mustApplyPostgresTLSDefault(t, in)
		if got != in {
			t.Fatalf("DSN with a non-empty first sslmode changed\n got %s\nwant %s", got, in)
		}
		cfg := mustPostgresConfig(t, got)
		if cfg.SSLMode != pq.SSLModeVerifyFull {
			t.Fatalf("SSLMode = %q, want verify-full", cfg.SSLMode)
		}
	})

	t.Run("repeated blank sslmode uses the remote default", func(t *testing.T) {
		in := "postgres://user:secret@db.example.com/whatsapp?sslmode=&sslmode="
		got := mustApplyPostgresTLSDefault(t, in)
		assertPostgresSSLMode(t, got, "verify-full")
		cfg := mustPostgresConfig(t, got)
		if cfg.SSLMode != pq.SSLModeVerifyFull {
			t.Fatalf("SSLMode = %q, want verify-full", cfg.SSLMode)
		}
	})

	t.Run("blank sslmode is not explicit", func(t *testing.T) {
		in := "postgres://user:secret@db.example.com/whatsapp?sslmode="
		got := mustApplyPostgresTLSDefault(t, in)
		assertPostgresSSLMode(t, got, "verify-full")
		cfg := mustPostgresConfig(t, got)
		if cfg.Host != "db.example.com" {
			t.Fatalf("dial host = %q, want db.example.com", cfg.Host)
		}
		if cfg.SSLMode != pq.SSLModeVerifyFull {
			t.Fatalf("SSLMode = %q, want verify-full", cfg.SSLMode)
		}
	})

	t.Run("fragment stays a fragment", func(t *testing.T) {
		in := "postgres://user:secret@db.example.com/whatsapp#frag"
		got := mustApplyPostgresTLSDefault(t, in)
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("parse result: %v", err)
		}
		if u.Fragment != "frag" {
			t.Fatalf("fragment = %q, want frag in %s", u.Fragment, got)
		}
		if u.Query().Get("sslmode") != "verify-full" {
			t.Fatalf("sslmode = %q, want verify-full in %s", u.Query().Get("sslmode"), got)
		}
		if strings.Contains(u.Fragment, "sslmode") {
			t.Fatalf("sslmode was appended to the fragment: %s", got)
		}
		cfg := mustPostgresConfig(t, got)
		if cfg.SSLMode != pq.SSLModeVerifyFull {
			t.Fatalf("SSLMode = %q, want verify-full", cfg.SSLMode)
		}
	})

	t.Run("localhost without override stays disable", func(t *testing.T) {
		in := "postgres://user:secret@localhost/whatsapp"
		got := mustApplyPostgresTLSDefault(t, in)
		assertPostgresSSLMode(t, got, "disable")
		cfg := mustPostgresConfig(t, got)
		if cfg.Host != "localhost" {
			t.Fatalf("dial host = %q, want localhost", cfg.Host)
		}
		if cfg.Hostaddr.IsValid() {
			t.Fatalf("dial hostaddr = %s, want none", cfg.Hostaddr.String())
		}
		if cfg.SSLMode != pq.SSLModeDisable {
			t.Fatalf("SSLMode = %q, want disable", cfg.SSLMode)
		}
	})

	t.Run("explicit require stays require", func(t *testing.T) {
		in := "postgres://user:secret@db.example.com/whatsapp?sslmode=require"
		got := mustApplyPostgresTLSDefault(t, in)
		if got != in {
			t.Fatalf("explicit sslmode changed\n got %s\nwant %s", got, in)
		}
		cfg := mustPostgresConfig(t, got)
		if cfg.Host != "db.example.com" {
			t.Fatalf("dial host = %q, want db.example.com", cfg.Host)
		}
		if cfg.SSLMode != pq.SSLModeRequire {
			t.Fatalf("SSLMode = %q, want require", cfg.SSLMode)
		}
	})
}

func TestApplyPostgresTLSDefaultErrorsOmitPassword(t *testing.T) {
	isolatePostgresDialEnv(t)

	for _, dsn := range []string{
		"postgres://user:secret@host:abc/db",   // url.Parse quotes the input
		"postgres:user:secret@host/db",         // lib/pq quotes an opaque URI as a key
		"postgres://user:99999#secret@host/db", // '#' leaves "99999" as an invalid port
	} {
		_, err := applyPostgresTLSDefault(dsn)
		if err == nil {
			t.Fatalf("applyPostgresTLSDefault(%q) succeeded, want an error", dsn)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "99999") {
			t.Fatalf("error for %q contains password text: %v", dsn, err)
		}
	}
}

func isolatePostgresDialEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"PGHOST", "PGHOSTADDR", "PGSSLMODE", "PGSERVICE"} {
		value, ok := os.LookupEnv(key)
		t.Cleanup(func() {
			if ok {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		})
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func mustApplyPostgresTLSDefault(t *testing.T, dsn string) string {
	t.Helper()
	got, err := applyPostgresTLSDefault(dsn)
	if err != nil {
		t.Fatalf("applyPostgresTLSDefault(%q): %v", dsn, err)
	}
	return got
}

func assertRemoteHostaddrRejected(t *testing.T, dsn string) {
	t.Helper()
	got, err := applyPostgresTLSDefault(dsn)
	if err == nil || !strings.Contains(err.Error(), "hostaddr 203.0.113.10 is remote") {
		t.Fatalf("applyPostgresTLSDefault(%q) = %q, %v; want remote hostaddr error", dsn, got, err)
	}
}

func assertPostgresSSLMode(t *testing.T, dsn, want string) {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse %q: %v", dsn, err)
	}
	if got := u.Query().Get("sslmode"); got != want {
		t.Fatalf("sslmode = %q, want %q in %s", got, want, dsn)
	}
}

func mustPostgresConfig(t *testing.T, dsn string) pq.Config {
	t.Helper()
	cfg, err := pq.NewConfig(dsn)
	if err != nil {
		t.Fatalf("pq.NewConfig(%q): %v", dsn, err)
	}
	return cfg
}

func TestSQLiteDatabasePathSurvivesPragmas(t *testing.T) {
	original := "file:storages/whatsapp.db"
	formatted := sqlite.FormatChatStorageURI(original, true, true)
	if !strings.Contains(formatted, "busy_timeout") {
		t.Fatalf("formatted uri dropped busy timeout: %s", formatted)
	}

	before, err := sqliteSessionTarget(original)
	if err != nil {
		t.Fatal(err)
	}
	after, err := sqliteSessionTarget(formatted)
	if err != nil {
		t.Fatal(err)
	}
	if before.skip || after.skip || before.path != after.path {
		t.Fatalf("path changed from %q (skip %v) to %q (skip %v) via %s", before.path, before.skip, after.path, after.skip, formatted)
	}
	if !before.create || !after.create {
		t.Fatal("session file should be created when missing")
	}
}

func TestSQLiteMemoryURIDoesNotCreateFile(t *testing.T) {
	target, err := sqliteSessionTarget("file:storages/should-not-exist.db?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	if !target.skip {
		t.Fatalf("memory uri was treated as a file path %q", target.path)
	}

	memory, err := sqliteSessionTarget("file::memory:")
	if err != nil {
		t.Fatal(err)
	}
	if !memory.skip {
		t.Fatalf(":memory: uri was treated as a file path %q", memory.path)
	}
}

func TestSQLiteNoCreateURIDoesNotCreateFileOrDirectory(t *testing.T) {
	for _, query := range []string{"mode=rw", "mode=ro", "immutable=1", "immutable=true", "immutable=YES", "immutable=on"} {
		dbPath := filepath.Join(t.TempDir(), "missing", "whatsapp.db")
		if err := SecureSQLiteFile("file:" + filepath.ToSlash(dbPath) + "?" + query); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if _, err := os.Stat(filepath.Dir(dbPath)); !os.IsNotExist(err) {
			t.Fatalf("%s created %s (stat error %v); SQLite would not create it", query, filepath.Dir(dbPath), err)
		}
	}

	target, err := sqliteSessionTarget("file:storages/whatsapp.db?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	if !target.create {
		t.Fatal("mode=rwc session file should be created when missing")
	}
}

func TestSQLiteURIFragmentIsIgnored(t *testing.T) {
	// SQLite ignores everything from '#', so mode=memory in the fragment does not apply.
	for _, uri := range []string{"file:storages/whatsapp.db#backup", "file:storages/whatsapp.db#backup?mode=memory"} {
		target, err := sqliteSessionTarget(uri)
		if err != nil {
			t.Fatal(err)
		}
		if target.skip || !target.create || target.path != filepath.Clean("storages/whatsapp.db") {
			t.Fatalf("%s: target = %+v, want storages/whatsapp.db created when missing", uri, target)
		}
	}
}

func TestSQLiteWindowsDriveLetterURI(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-letter URIs only apply on Windows")
	}
	for _, uri := range []string{"file:///C:/gowa/whatsapp.db", "file:/C:/gowa/whatsapp.db"} {
		target, err := sqliteSessionTarget(uri)
		if err != nil {
			t.Fatal(err)
		}
		if target.path != `C:\gowa\whatsapp.db` {
			t.Fatalf("%s path = %q, want C:\\gowa\\whatsapp.db", uri, target.path)
		}
	}
}
