//go:build !windows

package sysmetrics

import "syscall"

func filesystem(path string) Filesystem {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return Filesystem{}
	}
	total := uint64(stat.Blocks) * uint64(stat.Bsize)
	free := uint64(stat.Bavail) * uint64(stat.Bsize)
	return Filesystem{TotalBytes: &total, FreeBytes: &free}
}
