package retention

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var sweepCutoff = time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)

func writeAged(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func ageDir(t *testing.T, dir string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.Chtimes(dir, mtime, mtime))
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Behaviour: a file goes only when its modification time is strictly before
// the cutoff.
func TestSweepRemovesOnlyOldFiles(t *testing.T) {
	root := t.TempDir()
	oldF := filepath.Join(root, "d_1", "2026-11-20", "old.jpg")
	atF := filepath.Join(root, "d_1", "2026-11-23", "at.jpg")
	newF := filepath.Join(root, "d_2", "new.jpg")
	writeAged(t, oldF, sweepCutoff.Add(-time.Second))
	writeAged(t, atF, sweepCutoff)
	writeAged(t, newF, sweepCutoff.Add(time.Second))

	removed, err := SweepDir(root, sweepCutoff)
	require.NoError(t, err)
	require.Equal(t, 1, removed)
	require.False(t, exists(oldF))
	require.True(t, exists(atF))
	require.True(t, exists(newF))
}

// Security: a symlink inside the root is never followed and never deleted,
// so files it points at outside the root survive however old they are.
func TestSweepNeverFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "keep", "precious.txt")
	writeAged(t, victim, sweepCutoff.Add(-365*24*time.Hour))
	victimFile := filepath.Join(outside, "lone.txt")
	writeAged(t, victimFile, sweepCutoff.Add(-365*24*time.Hour))

	dirLink := filepath.Join(root, "d_1", "escape")
	require.NoError(t, os.MkdirAll(filepath.Dir(dirLink), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(outside, "keep"), dirLink))
	fileLink := filepath.Join(root, "d_1", "lone-link")
	require.NoError(t, os.Symlink(victimFile, fileLink))

	_, err := SweepDir(root, sweepCutoff)
	require.NoError(t, err)
	require.True(t, exists(victim), "a file behind a directory symlink survives")
	require.True(t, exists(victimFile), "a file behind a file symlink survives")
	require.True(t, exists(dirLink), "the symlink itself is left alone")
	require.True(t, exists(fileLink))
}

// Security: a root that is itself a symlink is refused outright.
func TestSweepRefusesASymlinkedRoot(t *testing.T) {
	outside := t.TempDir()
	victim := filepath.Join(outside, "old.txt")
	writeAged(t, victim, sweepCutoff.Add(-48*time.Hour))
	link := filepath.Join(t.TempDir(), "media")
	require.NoError(t, os.Symlink(outside, link))

	_, err := SweepDir(link, sweepCutoff)
	require.Error(t, err)
	require.True(t, exists(victim))
}

// Behaviour: directories below the root that are empty and themselves old
// are removed; the root never is; a freshly emptied or fresh empty directory
// waits (a writer may have just created it).
func TestSweepRemovesEmptyDirsButNotTheRoot(t *testing.T) {
	root := t.TempDir()
	oldEmpty := filepath.Join(root, "d_1", "2026-11-01")
	require.NoError(t, os.MkdirAll(oldEmpty, 0o755))
	ageDir(t, oldEmpty, sweepCutoff.Add(-time.Hour))
	freshEmpty := filepath.Join(root, "d_2", "today")
	require.NoError(t, os.MkdirAll(freshEmpty, 0o755))
	ageDir(t, freshEmpty, sweepCutoff.Add(time.Hour))
	withOldFile := filepath.Join(root, "d_3", "day")
	writeAged(t, filepath.Join(withOldFile, "a.jpg"), sweepCutoff.Add(-time.Hour))
	ageDir(t, withOldFile, sweepCutoff.Add(-time.Hour))
	ageDir(t, root, sweepCutoff.Add(-time.Hour))

	_, err := SweepDir(root, sweepCutoff)
	require.NoError(t, err)
	require.False(t, exists(oldEmpty))
	require.True(t, exists(freshEmpty))
	require.True(t, exists(withOldFile), "emptied just now, so its time is fresh: it waits for a later sweep")
	require.True(t, exists(root))

	// A sweep after the window has passed again takes it.
	later := time.Now().Add(time.Hour)
	if later.Before(sweepCutoff.Add(2 * time.Hour)) {
		later = sweepCutoff.Add(2 * time.Hour)
	}
	_, err = SweepDir(root, later)
	require.NoError(t, err)
	require.False(t, exists(withOldFile))
	require.True(t, exists(root), "the root is never removed")
	entries, _ := os.ReadDir(root)
	require.Len(t, entries, 0, "only empty old directories were left, and they went")
}

// Behaviour: a root that does not exist yet is not an error.
func TestSweepMissingRootIsFine(t *testing.T) {
	removed, err := SweepDir(filepath.Join(t.TempDir(), "nope"), sweepCutoff)
	require.NoError(t, err)
	require.Zero(t, removed)
}

// Security: an empty or filesystem-root path is refused.
func TestSweepRefusesDangerousRoots(t *testing.T) {
	for _, bad := range []string{"", "/", ".", "./", "..", "../media", string(filepath.Separator)} {
		_, err := SweepDir(bad, sweepCutoff)
		require.Error(t, err, "%q", bad)
	}
	_, err := SweepDir(t.TempDir(), time.Time{})
	require.Error(t, err, "a zero cutoff deletes nothing")
}

// Behaviour and security: in the storages folder only old history dumps go.
// The databases beside them, anything in a subfolder, a fresh dump and a
// symlink named like a dump are left alone.
func TestSweepHistoryDumpsTakesOnlyOldDumps(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	old := sweepCutoff.Add(-time.Hour)
	oldDump := filepath.Join(dir, "history-1-27820000001.0:1@s.whatsapp.net-1-INITIAL_BOOTSTRAP.json")
	freshDump := filepath.Join(dir, "history-2-x-1-RECENT.json")
	db := filepath.Join(dir, "whatsapp.db")
	accounts := filepath.Join(dir, "accounts.db")
	nested := filepath.Join(dir, "scheduled", "history-3.json")
	target := filepath.Join(outside, "precious.json")
	writeAged(t, oldDump, old)
	writeAged(t, freshDump, sweepCutoff.Add(time.Hour))
	writeAged(t, db, old)
	writeAged(t, accounts, old)
	writeAged(t, nested, old)
	writeAged(t, target, old)
	link := filepath.Join(dir, "history-9-link.json")
	require.NoError(t, os.Symlink(target, link))

	n, err := SweepHistoryDumps(dir, sweepCutoff)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	require.False(t, exists(oldDump))
	for _, kept := range []string{freshDump, db, accounts, nested, target, link} {
		require.True(t, exists(kept), kept)
	}
	n, err = SweepHistoryDumps(filepath.Join(dir, "missing"), sweepCutoff)
	require.NoError(t, err)
	require.Zero(t, n)
	_, err = SweepHistoryDumps(dir, time.Time{})
	require.Error(t, err)
}
