package whatsapp

import (
	"errors"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reservationSweepTestRepo records the cutoff the sweep asks for, so a test can
// assert the TTL window without reaching into the database.
type reservationSweepTestRepo struct {
	chatstorage.IChatStorageRepository
	calls   int
	cutoff  time.Time
	removed int64
	err     error
}

func (r *reservationSweepTestRepo) DeleteStaleChatwootMessageLinkReservations(olderThan time.Time) (int64, error) {
	r.calls++
	r.cutoff = olderThan
	return r.removed, r.err
}

// The reservation stub is rolled back in-process on every failure path, which a
// crash bypasses: the row survives with chatwoot_message_id == 0, and from then
// on decideChatwootLinkAction reads it as a delivery in flight and skips every
// redelivery. The retry worker sweeps those orphans, starting at boot — the
// moment the stubs of the killed process are waiting to be reclaimed.
func TestSweepStaleChatwootForwardReservations(t *testing.T) {
	cases := []struct {
		name      string
		repo      *reservationSweepTestRepo
		wantCalls int
	}{
		{"reclaims orphaned stubs", &reservationSweepTestRepo{removed: 2}, 1},
		{"nothing to reclaim", &reservationSweepTestRepo{}, 1},
		{"storage error is contained", &reservationSweepTestRepo{err: errors.New("db is gone")}, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := time.Now()
			sweepStaleChatwootForwardReservations(tc.repo)
			require.Equal(t, tc.wantCalls, tc.repo.calls)

			// Only stubs older than the TTL are orphans: a forward still running
			// keeps a fresh row and must never be swept out from under itself.
			assert.WithinDuration(t, before.Add(-chatwootForwardReservationTTL), tc.repo.cutoff, time.Minute)
			assert.Greater(t, chatwootForwardReservationTTL, time.Minute,
				"the window has to clear the slowest legitimate forward")
		})
	}

	t.Run("no storage configured is a no-op", func(t *testing.T) {
		assert.NotPanics(t, func() { sweepStaleChatwootForwardReservations(nil) })
	})
}
