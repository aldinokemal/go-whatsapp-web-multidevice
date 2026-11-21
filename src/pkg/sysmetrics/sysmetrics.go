// Package sysmetrics reports what an operator needs to size and watch a GOWA instance:
// memory of the process and of its container, disk used by media and storages, and the
// free space left on the volume. Everything is read from the runtime, /proc and the
// cgroup files; nothing is sampled in the background.
package sysmetrics

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Folder is the size of a directory tree.
type Folder struct {
	Bytes int64 `json:"bytes"`
	Files int64 `json:"files"`
}

// Filesystem is the volume a path lives on; nil fields mean "unknown on this platform".
type Filesystem struct {
	TotalBytes *uint64 `json:"total_bytes"`
	FreeBytes  *uint64 `json:"free_bytes"`
}

// Memory groups the Go runtime, the process and the container (cgroup) view.
type Memory struct {
	HeapAllocBytes   uint64  `json:"heap_alloc_bytes"`
	RuntimeSysBytes  uint64  `json:"runtime_sys_bytes"`
	ProcessRSSBytes  *uint64 `json:"process_rss_bytes"`
	ContainerBytes   *uint64 `json:"container_bytes"`
	ContainerLimit   *uint64 `json:"container_limit_bytes"`
	Goroutines       int     `json:"goroutines"`
	GarbageCollected uint32  `json:"gc_cycles"`
}

// Snapshot is what /app/metrics returns.
type Snapshot struct {
	StartedAt     time.Time         `json:"started_at"`
	UptimeSeconds int64             `json:"uptime_seconds"`
	GoVersion     string            `json:"go_version"`
	CPUSeconds    *float64          `json:"cpu_seconds"`
	Memory        Memory            `json:"memory"`
	Folders       map[string]Folder `json:"folders"`
	Filesystem    Filesystem        `json:"filesystem"`
}

var startedAt = time.Now()

// Folder sizes cost a full walk; media folders hold thousands of files, so a walk is
// reused for a minute.
const folderCacheFor = time.Minute

var (
	folderMu     sync.Mutex
	folderCache  map[string]Folder
	folderCached time.Time
)

// Collect builds a snapshot. folders maps a label to a path; fsPath is the path whose
// volume is reported.
func Collect(folders map[string]string, fsPath string) Snapshot {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	snapshot := Snapshot{
		StartedAt:     startedAt,
		UptimeSeconds: int64(time.Since(startedAt).Seconds()),
		GoVersion:     runtime.Version(),
		CPUSeconds:    processCPUSeconds(),
		Memory: Memory{
			HeapAllocBytes:   mem.HeapAlloc,
			RuntimeSysBytes:  mem.Sys,
			ProcessRSSBytes:  processRSS(),
			Goroutines:       runtime.NumGoroutine(),
			GarbageCollected: mem.NumGC,
		},
		Folders:    folderSizes(folders),
		Filesystem: filesystem(fsPath),
	}
	snapshot.Memory.ContainerBytes, snapshot.Memory.ContainerLimit = containerMemory()
	return snapshot
}

func folderSizes(folders map[string]string) map[string]Folder {
	folderMu.Lock()
	defer folderMu.Unlock()
	if folderCache != nil && time.Since(folderCached) < folderCacheFor {
		return folderCache
	}
	result := make(map[string]Folder, len(folders))
	for label, root := range folders {
		var size Folder
		_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return nil
			}
			if info, err := entry.Info(); err == nil {
				size.Bytes += info.Size()
				size.Files++
			}
			return nil
		})
		result[label] = size
	}
	folderCache, folderCached = result, time.Now()
	return result
}

// processRSS reads VmRSS from /proc (Linux); nil elsewhere.
func processRSS() *uint64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
					bytes := kb * 1024
					return &bytes
				}
			}
		}
	}
	return nil
}

// processCPUSeconds is user+system CPU time from /proc/self/stat (Linux); nil elsewhere.
func processCPUSeconds() *float64 {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return nil
	}
	// The command name may contain spaces; fields are counted after its closing ')'.
	text := string(data)
	end := strings.LastIndex(text, ")")
	if end < 0 {
		return nil
	}
	fields := strings.Fields(text[end+1:])
	if len(fields) < 13 {
		return nil
	}
	user, err1 := strconv.ParseFloat(fields[11], 64)
	system, err2 := strconv.ParseFloat(fields[12], 64)
	if err1 != nil || err2 != nil {
		return nil
	}
	const ticksPerSecond = 100 // USER_HZ on every Linux the images run on
	seconds := (user + system) / ticksPerSecond
	return &seconds
}

// containerMemory reads the cgroup v2 files, falling back to cgroup v1. A limit of
// "max" (no limit) is reported as nil.
func containerMemory() (current, limit *uint64) {
	read := func(path string) *uint64 {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		value := strings.TrimSpace(string(data))
		if value == "max" {
			return nil
		}
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return nil
		}
		return &n
	}
	if current = read("/sys/fs/cgroup/memory.current"); current != nil {
		return current, read("/sys/fs/cgroup/memory.max")
	}
	current = read("/sys/fs/cgroup/memory/memory.usage_in_bytes")
	limit = read("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	// cgroup v1 reports "no limit" as a huge number.
	if limit != nil && *limit >= 1<<62 {
		limit = nil
	}
	return current, limit
}
