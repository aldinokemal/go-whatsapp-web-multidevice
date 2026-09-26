package usecase

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLockLogin_SerializesPerDevice(t *testing.T) {
	svc := &serviceApp{}
	unlock, err := svc.lockLogin(context.Background(), "dev1")
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}

	// Another device is independent.
	unlockOther, err := svc.lockLogin(context.Background(), "dev2")
	if err != nil {
		t.Fatalf("other device lock: %v", err)
	}
	unlockOther()

	// A second login on the same device waits and gives up with its context.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := svc.lockLogin(ctx, "dev1"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded while locked, got %v", err)
	}

	unlock()
	unlockAgain, err := svc.lockLogin(context.Background(), "dev1")
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	unlockAgain()
}
