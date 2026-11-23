package retention

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

type fakeChats struct {
	mu                    sync.Mutex
	cutoffs               []time.Time
	block                 chan struct{} // when set, each call waits on it
	inFlight, maxInFlight atomic.Int32
	err                   error
	scheduledCutoffs      []time.Time
	scheduledErr          error
}

func (f *fakeChats) PurgeMessagesBefore(cutoff time.Time) (chatstorage.PurgeResult, error) {
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		m := f.maxInFlight.Load()
		if n <= m || f.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	f.mu.Lock()
	f.cutoffs = append(f.cutoffs, cutoff)
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	return chatstorage.PurgeResult{Messages: 3, Chats: 1}, f.err
}

func (f *fakeChats) PurgeFinishedScheduledSendsBefore(cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scheduledCutoffs = append(f.scheduledCutoffs, cutoff)
	return 2, f.scheduledErr
}

func (f *fakeChats) calls() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.cutoffs...)
}

type fakeAccounts struct{ calls atomic.Int32 }

func (f *fakeAccounts) PruneExpired(context.Context) (store.PruneCounts, error) {
	f.calls.Add(1)
	return store.PruneCounts{Sessions: 1}, nil
}

// Behaviour: the cutoff is exactly now minus the window, in UTC, whatever
// zone "now" is expressed in.
func TestRunOnceUsesTheWindow(t *testing.T) {
	chats, accts := &fakeChats{}, &fakeAccounts{}
	w := &Worker{Window: 24 * time.Hour, Chats: chats, Accounts: accts}
	now := time.Date(2026, time.November, 23, 14, 30, 0, 0, time.FixedZone("SAST", 2*3600))

	rep, err := w.RunOnce(context.Background(), now)
	require.NoError(t, err)
	got := chats.calls()
	require.Len(t, got, 1)
	require.Equal(t, time.Date(2026, time.November, 22, 12, 30, 0, 0, time.UTC), got[0])
	require.Equal(t, time.UTC, got[0].Location())
	require.Equal(t, int32(1), accts.calls.Load())
	require.Equal(t, int64(3), rep.Messages)
	require.Equal(t, int64(1), rep.Chats)
}

// Behaviour: no window (zero or negative) deletes nothing at all.
func TestZeroWindowDeletesNothing(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "old.jpg")
	writeAged(t, old, time.Now().Add(-1000*time.Hour))
	for _, win := range []time.Duration{0, -time.Hour} {
		chats, accts := &fakeChats{}, &fakeAccounts{}
		w := &Worker{Window: win, Chats: chats, Accounts: accts, FileRoots: []string{root}}
		_, err := w.RunOnce(context.Background(), time.Now())
		require.Error(t, err)
		require.Empty(t, chats.calls())
		require.Zero(t, accts.calls.Load())
		require.True(t, exists(old))
	}
}

// Behaviour: the file roots are swept with the same cutoff, and one failing
// step does not stop the others.
func TestRunOnceSweepsRootsAndCarriesOnAfterAFailure(t *testing.T) {
	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
	media, send := t.TempDir(), t.TempDir()
	oldMedia := filepath.Join(media, "d_1", "a.jpg")
	oldSend := filepath.Join(send, "b.pdf")
	fresh := filepath.Join(media, "d_1", "c.jpg")
	writeAged(t, oldMedia, now.Add(-25*time.Hour))
	writeAged(t, oldSend, now.Add(-25*time.Hour))
	writeAged(t, fresh, now.Add(-23*time.Hour))

	storages := t.TempDir()
	oldDump := filepath.Join(storages, "history-1-x-1-RECENT.json")
	writeAged(t, oldDump, now.Add(-25*time.Hour))

	chats, accts := &fakeChats{err: errors.New("db busy")}, &fakeAccounts{}
	w := &Worker{Window: 24 * time.Hour, Chats: chats, Accounts: accts, FileRoots: []string{media, send}, HistoryDumpDir: storages}
	rep, err := w.RunOnce(context.Background(), now)
	require.Error(t, err)
	require.Equal(t, 3, rep.Files)
	require.False(t, exists(oldDump))
	require.False(t, exists(oldMedia))
	require.False(t, exists(oldSend))
	require.True(t, exists(fresh))
	require.Equal(t, int32(1), accts.calls.Load())
}

// Mechanism: runs never overlap, however slow one is.
func TestRunDoesNotOverlap(t *testing.T) {
	chats := &fakeChats{block: make(chan struct{})}
	w := &Worker{Window: time.Hour, Chats: chats, Accounts: &fakeAccounts{}, Every: time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	// Let many ticks pass while the first run is stuck.
	require.Eventually(t, func() bool { return len(chats.calls()) == 1 }, time.Second, time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	require.Len(t, chats.calls(), 1, "no second run starts while the first is in flight")
	// Release runs one at a time.
	for i := 0; i < 3; i++ {
		chats.block <- struct{}{}
	}
	require.Eventually(t, func() bool { return len(chats.calls()) >= 4 }, time.Second, time.Millisecond)
	cancel()
	close(chats.block)
	<-done
	require.Equal(t, int32(1), chats.maxInFlight.Load())
}

// Mechanism: Run returns once its context is cancelled.
func TestRunStopsOnCancel(t *testing.T) {
	chats := &fakeChats{}
	w := &Worker{Window: time.Hour, Chats: chats, Accounts: &fakeAccounts{}, Every: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	require.Eventually(t, func() bool { return len(chats.calls()) == 1 }, time.Second, time.Millisecond, "runs once at start")
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancel")
	}
	require.Len(t, chats.calls(), 1)
}

// Security: a sweep error never carries a path (names can hold a phone number).
func TestSweepErrorsCarryNoPath(t *testing.T) {
	err := errors.Join(&fs.PathError{Op: "remove", Path: "statics/media/27825550100/x.jpg", Err: fs.ErrPermission}, errors.New("statics/media/27825550100"))
	got := pathless(err)
	require.NotContains(t, got, "27825550100")
	require.Contains(t, got, "remove")
}

// Behaviour: finished scheduled sends are purged with the same cutoff as the
// messages, and a failure there does not stop the other steps.
func TestRunOncePurgesFinishedScheduledSends(t *testing.T) {
	chats, accts := &fakeChats{scheduledErr: errors.New("boom")}, &fakeAccounts{}
	w := &Worker{Window: 24 * time.Hour, Chats: chats, Accounts: accts}
	now := time.Date(2026, time.November, 23, 14, 30, 0, 0, time.UTC)

	rep, err := w.RunOnce(context.Background(), now)
	require.Error(t, err)
	require.Equal(t, []time.Time{now.Add(-24 * time.Hour)}, chats.scheduledCutoffs)
	require.Equal(t, int64(2), rep.ScheduledSends)
	require.Equal(t, int64(3), rep.Messages)
	require.Equal(t, int32(1), accts.calls.Load(), "accounts are still pruned")
}
