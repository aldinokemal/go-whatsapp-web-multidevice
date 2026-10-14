package owners

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

// fakePairs behaves like the device manager: a device that is logged out is
// no longer paired.
type fakePairs struct {
	mu        sync.Mutex
	devices   []PairedDevice
	loggedOut []string
	logoutErr error
}

func (f *fakePairs) Paired() []PairedDevice {
	time.Sleep(2 * time.Millisecond) // widen the window two simultaneous guards would race in
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]PairedDevice(nil), f.devices...)
}

func (f *fakePairs) Logout(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loggedOut = append(f.loggedOut, id)
	if f.logoutErr != nil {
		return f.logoutErr
	}
	kept := f.devices[:0]
	for _, d := range f.devices {
		if d.ID != id {
			kept = append(kept, d)
		}
	}
	f.devices = kept
	return nil
}

func TestSecondUserCannotLinkTheSameNumber(t *testing.T) {
	st, _, _, _ := fixture(t) // d_a1 is a's, d_b1 is b's
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_b1", JID: "27825550999:12@s.whatsapp.net"}, // b just scanned with a's phone
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_b1")
	require.NoError(t, err)
	require.True(t, refused)
	require.Equal(t, []string{"d_b1"}, src.loggedOut, "the newcomer is logged out; the first owner keeps it")
}

func TestSameUserRelinkIsAllowed(t *testing.T) {
	st, _, _, _ := fixture(t) // d_a1 and d_a2 are both a's
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_a2", JID: "27825550999@s.whatsapp.net"},
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_a2")
	require.NoError(t, err)
	require.False(t, refused)
	require.Empty(t, src.loggedOut)
}

func TestDifferentNumbersAreAllowed(t *testing.T) {
	st, _, _, _ := fixture(t)
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_b1", JID: "27825550998@s.whatsapp.net"},
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_b1")
	require.NoError(t, err)
	require.False(t, refused)
	require.Empty(t, src.loggedOut)
}

func TestGuardFailsClosedWhenStoreFails(t *testing.T) {
	st, _, _, _ := fixture(t)
	require.NoError(t, st.Close())
	src := &fakePairs{devices: []PairedDevice{{ID: "d_b1", JID: "27825550999@s.whatsapp.net"}}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_b1")
	require.Error(t, err)
	require.True(t, refused, "an unreadable owner never keeps a link")
	require.Equal(t, []string{"d_b1"}, src.loggedOut)
}

// flakyOwners answers for some devices and fails for the rest.
type flakyOwners struct {
	owners map[string]string
	fail   map[string]bool
}

func (f flakyOwners) OwnerOf(_ context.Context, id string) (string, error) {
	if f.fail[id] {
		return "", errors.New("database is locked")
	}
	if o, ok := f.owners[id]; ok {
		return o, nil
	}
	return "", store.ErrNotFound
}
func (flakyOwners) Audit(context.Context, store.AuditEvent) error { return nil }

func TestGuardFailsClosedWhenSiblingLookupFails(t *testing.T) {
	owners := flakyOwners{owners: map[string]string{"d_b1": "u_b"}, fail: map[string]bool{"d_a1": true}}
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_b1", JID: "27825550999@s.whatsapp.net"},
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, owners, "d_b1")
	require.Error(t, err)
	require.True(t, refused)
	require.Equal(t, []string{"d_b1"}, src.loggedOut)
}

func TestGuardIgnoresUnownedSibling(t *testing.T) {
	owners := flakyOwners{owners: map[string]string{"d_b1": "u_b"}}
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_legacy", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_b1", JID: "27825550999@s.whatsapp.net"},
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, owners, "d_b1")
	require.NoError(t, err)
	require.False(t, refused)
}

func TestGuardRefusesNonPhoneJIDs(t *testing.T) {
	st, _, _, _ := fixture(t)
	for name, devices := range map[string][]PairedDevice{
		"new link is a LID": {
			{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
			{ID: "d_b1", JID: "123456789012345@lid"},
		},
		"sibling is a LID": {
			{ID: "d_a1", JID: "123456789012345@lid"},
			{ID: "d_b1", JID: "27825550999@s.whatsapp.net"},
		},
		"new link is garbage": {
			{ID: "d_b1", JID: "not a jid@@"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			src := &fakePairs{devices: devices}
			refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_b1")
			require.NoError(t, err)
			require.True(t, refused, "a number we cannot read is never allowed through")
			require.Equal(t, []string{"d_b1"}, src.loggedOut)
		})
	}
}

func TestGuardReportsLogoutFailure(t *testing.T) {
	st, _, _, _ := fixture(t)
	src := &fakePairs{logoutErr: errors.New("socket closed"), devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_b1", JID: "27825550999@s.whatsapp.net"},
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_b1")
	require.True(t, refused)
	require.ErrorContains(t, err, "socket closed", "the caller must retry or purge")
}

func TestSimultaneousLinksLeaveExactlyOne(t *testing.T) {
	for i := 0; i < 20; i++ {
		st, _, _, _ := fixture(t)
		src := &fakePairs{devices: []PairedDevice{
			{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
			{ID: "d_b1", JID: "27825550999@s.whatsapp.net"},
		}}
		var wg sync.WaitGroup
		for _, id := range []string{"d_a1", "d_b1"} {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				_, _ = GuardDuplicatePhone(context.Background(), src, st, id)
			}(id)
		}
		wg.Wait()
		require.Len(t, src.loggedOut, 1, "never both")
		require.Len(t, src.Paired(), 1, "exactly one stays linked")
	}
}
