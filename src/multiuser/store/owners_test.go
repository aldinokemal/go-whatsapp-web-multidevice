package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnership(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550101")

	require.NoError(t, st.SetOwner(ctx, "d_a1", a.ID))
	require.NoError(t, st.SetOwner(ctx, "d_b1", b.ID))
	require.Error(t, st.SetOwner(ctx, "d_a1", b.ID), "a device has one owner; no silent takeover")

	owner, err := st.OwnerOf(ctx, "d_a1")
	require.NoError(t, err)
	require.Equal(t, a.ID, owner)
	_, err = st.OwnerOf(ctx, "d_nobody")
	require.ErrorIs(t, err, ErrNotFound)

	ids, err := st.DevicesOf(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"d_a1"}, ids)

	n, _ := st.OwnedDeviceCount(ctx)
	require.Equal(t, 2, n)
	require.NoError(t, st.RemoveOwner(ctx, "d_a1"))
	ids, _ = st.DevicesOf(ctx, a.ID)
	require.Empty(t, ids)
}
