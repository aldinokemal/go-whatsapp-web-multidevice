package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Behaviour: only slots that never held a number, and are older than the
// cutoff, are listed and can be expired; a slot signed out from the phone, a
// refused one, a connected one and a fresh one are all kept.
func TestNeverLinkedSlotsAreTheOnlyOnesExpired(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return base }
	u, err := st.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	for _, id := range []string{"d_stale", "d_signed_out", "d_refused", "d_linked"} {
		require.NoError(t, st.SetOwner(ctx, id, u.ID))
	}
	require.NoError(t, st.MarkSignedOut(ctx, "d_signed_out", "27825550101", ""))
	require.NoError(t, st.RecordRefusal(ctx, "d_refused", RefusalDuplicate))
	require.NoError(t, st.RecordIdentity(ctx, "d_linked", "27825550199", "Shop"))
	st.Now = func() time.Time { return base.Add(50 * time.Minute) }
	require.NoError(t, st.SetOwner(ctx, "d_fresh", u.ID))

	cutoff := base.Add(30 * time.Minute)
	got, err := st.NeverLinkedSlotsBefore(ctx, cutoff)
	require.NoError(t, err)
	require.Equal(t, []OwnedSlot{{DeviceID: "d_stale", UserID: u.ID}}, got)

	for _, id := range []string{"d_signed_out", "d_refused", "d_linked", "d_fresh"} {
		ok, err := st.ExpireNeverLinkedSlot(ctx, id, cutoff)
		require.NoError(t, err)
		require.False(t, ok, id)
	}
	ok, err := st.ExpireNeverLinkedSlot(ctx, "d_stale", cutoff)
	require.NoError(t, err)
	require.True(t, ok)
	ids, err := st.DevicesOf(ctx, u.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"d_signed_out", "d_refused", "d_linked", "d_fresh"}, ids)
}

// Mechanism: a slot that gets a number between the listing and the expiry is
// not taken (the expiry re-checks in the same statement as the delete).
func TestExpiryRechecksTheSlot(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	base := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return base }
	u, err := st.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	require.NoError(t, st.SetOwner(ctx, "d_x", u.ID))
	cutoff := base.Add(time.Hour)
	got, err := st.NeverLinkedSlotsBefore(ctx, cutoff)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NoError(t, st.RecordIdentity(ctx, "d_x", "27825550101", ""))
	ok, err := st.ExpireNeverLinkedSlot(ctx, "d_x", cutoff)
	require.NoError(t, err)
	require.False(t, ok)
}
