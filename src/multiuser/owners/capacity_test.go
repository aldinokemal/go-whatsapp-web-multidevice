package owners

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

// lockedInner is innerStub made safe for concurrent AddDevice calls.
type lockedInner struct {
	*innerStub
	mu sync.Mutex
}

func (l *lockedInner) AddDevice(ctx context.Context, id string, w *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.innerStub.AddDevice(ctx, id, w)
}

func (l *lockedInner) RemoveDevice(ctx context.Context, id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.innerStub.RemoveDevice(ctx, id)
}

func (l *lockedInner) addedCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.added)
}

func capped(inner domainDevice.IDeviceUsecase, st *store.Store, max int) *ScopedDevices {
	return NewScopedDevicesCapped(inner, st, func() int { return max }).(*ScopedDevices)
}

func newUser(t *testing.T, st *store.Store, i int) *store.User {
	t.Helper()
	u, err := st.CreateUser(context.Background(), fmt.Sprintf("+278255%05d", i))
	require.NoError(t, err)
	return u
}

// Behaviour.
func TestServerCapRefusesTheNextLink(t *testing.T) {
	st, _, a, b := fixture(t) // three numbers linked: a has 2, b has 1
	inner := &innerStub{}
	svc := capped(inner, st, 3)
	_ = a
	c := newUser(t, st, 7)
	_, err := svc.AddDevice(auth.WithUser(context.Background(), c), "", nil)
	require.ErrorIs(t, err, ErrServerFull)
	require.Equal(t, "There's no room for another number right now. Try again later.", err.Error())
	require.Empty(t, inner.added, "no slot is created when the server is full")
	_, err = svc.AddDevice(auth.WithUser(context.Background(), b), "", nil)
	require.ErrorIs(t, err, ErrServerFull, "under their own limit, but the server is full")
}

func TestUserLimitStillApplies(t *testing.T) {
	st, _, a, _ := fixture(t) // a has 2 of 2
	inner := &innerStub{}
	svc := capped(inner, st, 100)
	_, err := svc.AddDevice(auth.WithUser(context.Background(), a), "", nil)
	require.ErrorIs(t, err, ErrDeviceLimitReached)
	require.Empty(t, inner.added)
}

func TestCapZeroMeansNoCap(t *testing.T) {
	st, _, _, b := fixture(t)
	for i, svc := range []domainDevice.IDeviceUsecase{
		capped(&innerStub{}, st, 0),
		NewScopedDevices(&innerStub{}, st), // no MaxTotal at all
	} {
		c := newUser(t, st, i)
		_, err := svc.AddDevice(auth.WithUser(context.Background(), c), "", nil)
		require.NoError(t, err)
	}
	_ = b
}

func TestRemovingANumberFreesASlot(t *testing.T) {
	st, _, a, _ := fixture(t)
	svc := capped(&innerStub{}, st, 3)
	c := newUser(t, st, 7)
	ctxC := auth.WithUser(context.Background(), c)
	_, err := svc.AddDevice(ctxC, "", nil)
	require.ErrorIs(t, err, ErrServerFull)
	require.NoError(t, svc.RemoveDevice(auth.WithUser(context.Background(), a), "d_a1"))
	_, err = svc.AddDevice(ctxC, "", nil)
	require.NoError(t, err)
}

// Mechanism: the cap is a concurrency ceiling. reserveFunc is the step under
// test, so the harness can also be pointed at a deliberately broken one.
func raceForLastSlot(t *testing.T, mk func(*store.Store) reserveFunc) (wins, full int, owners int, slots int) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db")) // a file DB: real locking
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	for i := 0; i < 4; i++ { // 4 of 5 linked
		require.NoError(t, st.SetOwner(ctx, fmt.Sprintf("d_seed%d", i), newUser(t, st, 90000+i).ID))
	}
	inner := &lockedInner{innerStub: &innerStub{}}
	svc := capped(inner, st, 5)
	if mk != nil {
		svc.reserve = mk(st)
	}
	users := make([]*store.User, 20)
	for i := range users {
		users[i] = newUser(t, st, i)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	start := make(chan struct{})
	for _, u := range users {
		wg.Add(1)
		go func(u *store.User) {
			defer wg.Done()
			<-start
			_, err := svc.AddDevice(auth.WithUser(ctx, u), "", nil)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case err == ErrServerFull:
				full++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(u)
	}
	close(start)
	wg.Wait()
	owners, err = st.OwnedDeviceCount(ctx)
	require.NoError(t, err)
	return wins, full, owners, inner.addedCount()
}

func TestLastSlotUnderConcurrency(t *testing.T) {
	wins, full, owners, slots := raceForLastSlot(t, nil)
	require.Equal(t, 1, wins, "exactly one person gets the last slot")
	require.Equal(t, 19, full, "everyone else is told the server is full")
	require.Equal(t, 5, owners, "device_owners never passes the cap")
	require.Equal(t, 1, slots, "exactly one slot was created")
}

// The harness above can fail: with the count and the insert split into two
// statements (a check-then-act race) it over-admits.
func TestLastSlotHarnessCatchesASplitCheck(t *testing.T) {
	split := func(st *store.Store) reserveFunc {
		return func(ctx context.Context, deviceID, userID string, maxPerUser, maxTotal int) (store.Reservation, error) {
			mine, err := st.DevicesOf(ctx, userID)
			if err != nil {
				return 0, err
			}
			total, err := st.OwnedDeviceCount(ctx)
			if err != nil {
				return 0, err
			}
			if len(mine) >= maxPerUser {
				return store.UserLimit, nil
			}
			if maxTotal > 0 && total >= maxTotal {
				return store.ServerFull, nil
			}
			time.Sleep(2 * time.Millisecond) // the gap between check and act
			return store.Reserved, st.SetOwner(ctx, deviceID, userID)
		}
	}
	wins, _, owners, _ := raceForLastSlot(t, split)
	require.Greater(t, wins, 1, "the split check must over-admit, or the harness proves nothing")
	require.Greater(t, owners, 5)
}
