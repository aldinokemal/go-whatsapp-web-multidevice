package whatsapp

import (
	"net/url"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
)

func TestApplyPostgresTLSDefault(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantMode string
		same     bool
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
			name:     "userinfo is not the host",
			dsn:      "postgres://localhost:secret@db.example.com/whatsapp",
			wantMode: "verify-full",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applyPostgresTLSDefault(tc.dsn)
			if err != nil {
				t.Fatalf("applyPostgresTLSDefault: %v", err)
			}
			if tc.same {
				if got != tc.dsn {
					t.Fatalf("explicit sslmode changed\n got %s\nwant %s", got, tc.dsn)
				}
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
