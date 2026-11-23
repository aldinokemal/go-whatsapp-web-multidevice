package retention

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SweepDir deletes regular files below root whose modification time is
// strictly before cutoff, then removes directories below root that are empty
// and whose own modification time is also before cutoff (a directory a writer
// has just created, or one this sweep just emptied, waits for a later sweep).
//
// It never follows a symlink: a link inside root is neither followed nor
// deleted, and a root that is itself a symlink is refused. root itself is
// never removed. A root that does not exist is not an error.
func SweepDir(root string, cutoff time.Time) (removed int, err error) {
	if cutoff.IsZero() {
		return 0, errors.New("sweep needs a cutoff")
	}
	clean := filepath.Clean(root)
	if root == "" || clean == "." || clean == string(filepath.Separator) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return 0, fmt.Errorf("refusing to sweep %q", root)
	}
	info, err := os.Lstat(clean)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
		return 0, fmt.Errorf("refusing to sweep %q: not a plain directory", root)
	}

	var dirs []string
	var errs []error
	walkErr := filepath.WalkDir(clean, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable entry is skipped; the rest of the sweep goes on.
			if path != clean {
				errs = append(errs, err)
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			return err
		}
		if path == clean {
			return nil
		}
		if !inside(clean, path) {
			return fs.SkipDir // cannot happen without following links; belt and braces
		}
		switch t := d.Type(); {
		case t&fs.ModeSymlink != 0:
			return nil // never followed, never deleted
		case d.IsDir():
			dirs = append(dirs, path)
			return nil
		case !t.IsRegular():
			return nil // sockets, devices, pipes: not ours
		}
		fi, err := d.Info()
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		if fi.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
				return nil
			}
			removed++
		}
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}

	// Deepest first, so a parent is judged after its children have gone.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		fi, err := os.Lstat(dir)
		if err != nil || fi.Mode()&fs.ModeSymlink != 0 || !fi.IsDir() || !fi.ModTime().Before(cutoff) {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			continue
		}
		// os.Remove on a directory only succeeds when it is still empty.
		_ = os.Remove(dir)
	}
	return removed, errors.Join(errs...)
}

// inside reports whether path is strictly below root (both cleaned).
func inside(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// historyDumpPattern matches the raw history sync dumps upstream writes to
// the storages folder (history-<start>-<jid>-<n>-<type>.json).
const historyDumpPattern = "history-*.json"

// SweepHistoryDumps deletes history sync dumps directly in dir (never in a
// subfolder, never anything else: the databases live beside them) whose
// modification time is strictly before cutoff. Symlinks are left alone. A
// missing dir is not an error.
func SweepHistoryDumps(dir string, cutoff time.Time) (removed int, err error) {
	if cutoff.IsZero() {
		return 0, errors.New("sweep needs a cutoff")
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var errs []error
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if ok, _ := filepath.Match(historyDumpPattern, e.Name()); !ok {
			continue
		}
		fi, err := e.Info()
		if err != nil || !fi.ModTime().Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}
