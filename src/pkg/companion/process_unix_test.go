//go:build unix

package companion

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAdapterIsolatedFromTerminalProcessGroup(t *testing.T) {
	adapter, err := NewProcess([]string{os.Args[0], "-test.run=TestAdapterHelper", "--", "companion-helper", "normal"})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	child := adapter.(*ProcessAdapter).cmd.Process.Pid
	group, err := syscall.Getpgid(child)
	if err != nil || group != child || group == syscall.Getpgrp() {
		t.Fatal("adapter shares terminal signal group", group, child, err)
	}
}

func TestAdapterSignalFinishesCleanupBeforeExit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOWA_COMPANION_TEST_EVIDENCE", root)
	adapter, err := NewProcess([]string{os.Args[0], "-test.run=TestAdapterHelper", "--", "companion-helper", "cooperative-signal"})
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	done := make(chan error, 1)
	go func() { _, err := adapter.Call(context.Background(), "enter_code", Object{}); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "started")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("adapter did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := adapter.(*ProcessAdapter).cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("interrupted operation unexpectedly succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("signal did not cancel operation")
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "restored")); err != nil {
		t.Fatal("signal skipped cleanup", err)
	}
}
