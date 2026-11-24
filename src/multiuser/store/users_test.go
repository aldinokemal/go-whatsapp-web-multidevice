package store

import (
	"context"
	"fmt"
	"testing"
	"time"

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

func TestCreateUserWithLimit(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, err := st.CreateUserWithLimit(ctx, "+27825550100", 5)
	require.NoError(t, err)
	require.Equal(t, 5, u.MaxDevices)
	again, _ := st.UserByID(ctx, u.ID)
	require.Equal(t, 5, again.MaxDevices)
	for _, bad := range []int{0, -1, MaxDevicesLimit + 1} {
		_, err := st.CreateUserWithLimit(ctx, "+27825550101", bad)
		require.ErrorIs(t, err, ErrInvalid, bad)
	}
}

func seedUsers(t *testing.T, st *Store, n int) {
	t.Helper()
	tick := clockAt(st, t0)
	for i := 0; i < n; i++ {
		_, err := st.CreateUser(context.Background(), fmt.Sprintf("+278255%05d", i))
		require.NoError(t, err)
		tick(time.Minute)
	}
}

func TestUsersPageTotalsMatches(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	seedUsers(t, st, 120)

	total, items, err := st.ListUsersPage(ctx, UsersQuery{Limit: 50})
	require.NoError(t, err)
	require.Equal(t, 120, total)
	require.Len(t, items, 50)
	require.Equal(t, "+27825500119", items[0].Phone, "newest first by default")

	_, items, _ = st.ListUsersPage(ctx, UsersQuery{Offset: 100, Limit: 50})
	require.Len(t, items, 20)

	total, items, _ = st.ListUsersPage(ctx, UsersQuery{Search: "0100", Limit: 50})
	require.Equal(t, 1, total)
	require.Equal(t, "+27825500100", items[0].Phone)

	total, _, _ = st.ListUsersPage(ctx, UsersQuery{Search: "+27 82 55 0010", Limit: 50})
	require.Equal(t, 10, total, "spaces and + are stripped: 0010x matches ten")

	total, items, err = st.ListUsersPage(ctx, UsersQuery{Search: "abc", Limit: 50})
	require.NoError(t, err)
	require.Zero(t, total, "a search with no digits matches nobody")
	require.NotNil(t, items)
	require.Empty(t, items)

	total, _, _ = st.ListUsersPage(ctx, UsersQuery{Search: "%", Limit: 50})
	require.Zero(t, total, "LIKE wildcards are not passed through")

	u, _ := st.UserByPhone(ctx, "+27825500003")
	require.NoError(t, st.SetUserStatus(ctx, u.ID, StatusDisabled))
	total, items, _ = st.ListUsersPage(ctx, UsersQuery{Status: StatusDisabled, Limit: 50})
	require.Equal(t, 1, total)
	require.Equal(t, u.ID, items[0].ID)
	total, _, _ = st.ListUsersPage(ctx, UsersQuery{Status: StatusActive, Limit: 50})
	require.Equal(t, 119, total)
}

func TestUsersPageSorts(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	seedUsers(t, st, 3)
	u1, _ := st.UserByPhone(ctx, "+27825500001")
	require.NoError(t, st.TouchLogin(ctx, u1.ID))

	_, items, _ := st.ListUsersPage(ctx, UsersQuery{Sort: "phone", Limit: 50})
	require.Equal(t, "+27825500000", items[0].Phone)
	_, items, _ = st.ListUsersPage(ctx, UsersQuery{Sort: "phone", Desc: true, Limit: 50})
	require.Equal(t, "+27825500002", items[0].Phone)
	_, items, _ = st.ListUsersPage(ctx, UsersQuery{Sort: "last_login", Desc: true, Limit: 50})
	require.Equal(t, u1.ID, items[0].ID, "signed-in users first")
	_, items, _ = st.ListUsersPage(ctx, UsersQuery{Sort: "last_login", Limit: 50})
	require.Equal(t, u1.ID, items[0].ID, "never signed in sorts last either way")
}

// Security: the sort key never reaches SQL unless it is on the allowlist.
func TestUsersSortIsAllowlisted(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	seedUsers(t, st, 3)
	_, want, err := st.ListUsersPage(ctx, UsersQuery{Sort: "created", Desc: true, Limit: 50})
	require.NoError(t, err)
	for _, bad := range []string{"id; DROP TABLE users", "phone DESC, (SELECT 1)", "status"} {
		total, items, err := st.ListUsersPage(ctx, UsersQuery{Sort: bad, Desc: true, Limit: 50})
		require.NoError(t, err, bad)
		require.Equal(t, 3, total)
		require.Equal(t, want, items, "falls back to created")
	}
	total, _, err := st.ListUsersPage(ctx, UsersQuery{Status: "x' OR 1=1 --", Limit: 50})
	require.NoError(t, err)
	require.Equal(t, 3, total, "an unknown status filter means all")
}
