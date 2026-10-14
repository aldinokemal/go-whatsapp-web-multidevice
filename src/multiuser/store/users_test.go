package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateAndFindUser(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, err)
	require.Equal(t, StatusActive, u.Status)
	require.Equal(t, 2, u.MaxDevices)

	byPhone, err := st.UserByPhone(ctx, "+27825550100")
	require.NoError(t, err)
	require.Equal(t, u.ID, byPhone.ID)

	_, err = st.CreateUser(ctx, "+27825550100")
	require.Error(t, err, "phone is unique")

	_, err = st.UserByPhone(ctx, "+10000000000")
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, st.SetUserStatus(ctx, u.ID, StatusDisabled))
	again, err := st.UserByID(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, again.Status)

	require.Error(t, st.SetUserStatus(ctx, u.ID, "superuser"), "unknown status refused")
}
