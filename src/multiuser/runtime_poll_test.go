package multiuser

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
)

// fakePollPairSource is a owners.PairSource whose one device's JID can be
// flipped from unreadable ("", simulating "not settled yet") to a readable
// phone-number JID, on demand, from another goroutine.
type fakePollPairSource struct {
	mu    sync.Mutex
	id    string
	ready bool
}

func (f *fakePollPairSource) setReady(v bool) {
	f.mu.Lock()
	f.ready = v
	f.mu.Unlock()
}

func (f *fakePollPairSource) Paired() []owners.PairedDevice {
	f.mu.Lock()
	defer f.mu.Unlock()
	jid := ""
	if f.ready {
		jid = "27825550101@s.whatsapp.net"
	}
	return []owners.PairedDevice{{ID: f.id, JID: jid}}
}

func (f *fakePollPairSource) Logout(context.Context, string) error { return nil }

// TestWaitForPhoneNumberOrDeadlineReturnsAsSoonAsReady proves the poll exits
// the moment the phone number becomes readable, rather than always waiting
// out the full ceiling - the whole point of replacing the flat sleep.
func TestWaitForPhoneNumberOrDeadlineReturnsAsSoonAsReady(t *testing.T) {
	src := &fakePollPairSource{id: "d_x"}
	const becomesReadyAfter = 150 * time.Millisecond
	go func() {
		time.Sleep(becomesReadyAfter)
		src.setReady(true)
	}()

	start := time.Now()
	waitForPhoneNumberOrDeadline(src, "d_x", 2*time.Second, 25*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed < becomesReadyAfter {
		t.Fatalf("returned in %s, before the fake even became ready at %s - the poll can't be checking the real condition", elapsed, becomesReadyAfter)
	}
	if elapsed > 700*time.Millisecond {
		t.Fatalf("took %s to notice readiness at %s; expected to return shortly after, well under the 2s ceiling", elapsed, becomesReadyAfter)
	}
}

// TestWaitForPhoneNumberOrDeadlineNeverExceedsTheCeiling proves that when the
// number never becomes readable, the poll still returns once its deadline
// passes - nothing waits longer than the ceiling the flat sleep used to
// enforce.
func TestWaitForPhoneNumberOrDeadlineNeverExceedsTheCeiling(t *testing.T) {
	src := &fakePollPairSource{id: "d_x"} // never ready
	const deadline = 300 * time.Millisecond

	start := time.Now()
	waitForPhoneNumberOrDeadline(src, "d_x", deadline, 25*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed < deadline {
		t.Fatalf("returned after %s, before its %s deadline, though the number was never ready", elapsed, deadline)
	}
	if elapsed > deadline+200*time.Millisecond {
		t.Fatalf("waited %s for a %s deadline - exceeded the ceiling by more than scheduling jitter should allow", elapsed, deadline)
	}
}

// TestWaitForPhoneNumberOrDeadlineReturnsImmediatelyIfAlreadyReady proves the
// common happy path - the number is already readable the first time it is
// checked - costs effectively nothing, not even one poll interval.
func TestWaitForPhoneNumberOrDeadlineReturnsImmediatelyIfAlreadyReady(t *testing.T) {
	src := &fakePollPairSource{id: "d_x"}
	src.setReady(true)

	start := time.Now()
	waitForPhoneNumberOrDeadline(src, "d_x", 2*time.Second, 100*time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 50*time.Millisecond {
		t.Fatalf("took %s to notice an already-ready number; expected an immediate return", elapsed)
	}
}
