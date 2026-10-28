//go:build unix

package companion

import (
	"golang.org/x/sys/unix"
	"os"
)

type fileLock struct{ file *os.File }

func acquireLock(path string) (*fileLock, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, block("lock_busy")
	}
	return &fileLock{f}, nil
}
func (l *fileLock) Close() error { return l.file.Close() }
