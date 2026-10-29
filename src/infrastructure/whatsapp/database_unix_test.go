//go:build unix

package whatsapp

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	waLog "go.mau.fi/whatsmeow/util/log"
)

func TestSQLiteOpenCreatesOwnerOnlySessionFile(t *testing.T) {
	// Umask is process-wide. This package's tests do not call t.Parallel, and
	// this test restores the previous mask before returning.
	old := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(old) })

	t.Run("new file", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "sessions", "whatsapp.db")
		openSQLiteSession(t, "file:"+dbPath)
		assertSQLiteWrote(t, dbPath)
		assertOwnerOnly(t, dbPath, 0600)
		assertOwnerOnly(t, filepath.Dir(dbPath), 0700)
		assertSidecarsOwnerOnly(t, dbPath)
	})

	t.Run("existing loose file and directory", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "sessions")
		if err := os.MkdirAll(dir, 0777); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0777); err != nil {
			t.Fatal(err)
		}
		dbPath := filepath.Join(dir, "whatsapp.db")
		if err := os.WriteFile(dbPath, nil, 0666); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dbPath, 0666); err != nil {
			t.Fatal(err)
		}
		if perm := filePerm(t, dbPath); perm&0077 == 0 {
			t.Fatalf("fixture mode %o has no group or other bits; umask hid a loose create", perm)
		}
		if perm := filePerm(t, dir); perm&0077 == 0 {
			t.Fatalf("fixture directory mode %o has no group or other bits", perm)
		}
		openSQLiteSession(t, "file:"+dbPath)
		assertSQLiteWrote(t, dbPath)
		assertOwnerOnly(t, dbPath, 0600)
		// An existing directory may be shared or owned by someone else.
		if perm := filePerm(t, dir); perm != 0777 {
			t.Fatalf("existing directory mode changed to %o, want 0777", perm)
		}
		assertSidecarsOwnerOnly(t, dbPath)
	})
}

func openSQLiteSession(t *testing.T, uri string) {
	t.Helper()
	container, err := initDatabase(context.Background(), waLog.Noop, uri)
	if err != nil {
		t.Fatalf("initDatabase: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Close(); err != nil {
			t.Errorf("close session database: %v", err)
		}
	})
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func assertSQLiteWrote(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatalf("%s was not written by sqlite", path)
	}
}

func assertOwnerOnly(t *testing.T, path string, wantOwner os.FileMode) {
	t.Helper()
	perm := filePerm(t, path)
	if perm&0077 != 0 {
		t.Fatalf("%s mode %o includes group or other permissions", path, perm)
	}
	if perm&wantOwner != wantOwner {
		t.Fatalf("%s mode %o is missing owner bits %o", path, perm, wantOwner)
	}
}

func assertSidecarsOwnerOnly(t *testing.T, dbPath string) {
	t.Helper()
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		path := dbPath + suffix
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		assertOwnerOnly(t, path, 0600)
	}
}
