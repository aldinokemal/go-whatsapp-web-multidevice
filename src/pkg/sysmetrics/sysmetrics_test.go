package sysmetrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCollectSizesFoldersAndReportsTheVolume(t *testing.T) {
	folderCache = nil
	root := t.TempDir()
	media := filepath.Join(root, "media", "5511900000000", "2026-10-12")
	if err := os.MkdirAll(media, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int{"a.mp4": 1000, "b.jpg": 24} {
		if err := os.WriteFile(filepath.Join(media, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	snapshot := Collect(map[string]string{"media": filepath.Join(root, "media"), "missing": filepath.Join(root, "nope")}, root)

	if got := snapshot.Folders["media"]; got.Bytes != 1024 || got.Files != 2 {
		t.Fatalf("media = %+v, want 1024 bytes in 2 files", got)
	}
	if got := snapshot.Folders["missing"]; got.Bytes != 0 || got.Files != 0 {
		t.Fatalf("missing folder = %+v, want zero", got)
	}
	if snapshot.Filesystem.TotalBytes == nil || snapshot.Filesystem.FreeBytes == nil {
		t.Fatal("filesystem of the temp dir should be known")
	}
	if snapshot.Memory.HeapAllocBytes == 0 || snapshot.Memory.Goroutines == 0 {
		t.Fatal("runtime memory should be reported")
	}

	// A second call within the minute reuses the walk.
	if err := os.WriteFile(filepath.Join(media, "c.bin"), make([]byte, 10), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Collect(map[string]string{"media": filepath.Join(root, "media")}, root).Folders["media"]; got.Files != 2 {
		t.Fatalf("cache not reused: %+v", got)
	}
	folderCached = time.Now().Add(-2 * folderCacheFor)
	if got := Collect(map[string]string{"media": filepath.Join(root, "media")}, root).Folders["media"]; got.Files != 3 {
		t.Fatalf("expired cache not refreshed: %+v", got)
	}
}
