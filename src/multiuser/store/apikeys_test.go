package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func keyUser(t *testing.T, st *Store, phone string) *User {
	t.Helper()
	u, err := st.CreateUser(context.Background(), phone)
	require.NoError(t, err)
	return u
}

// Behaviour: a new key authenticates as its owner, and is shaped as promised.
func TestAPIKeyRoundTrip(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := keyUser(t, st, "+27825550101")

	raw, k, err := st.CreateAPIKey(ctx, alice.ID, "  Laptop script ")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(raw, APIKeyPrefix))
	require.Len(t, raw, len(APIKeyPrefix)+43)
	require.Equal(t, "Laptop script", k.Name)
	require.Equal(t, raw[len(APIKeyPrefix):len(APIKeyPrefix)+6], k.Prefix)
	require.Nil(t, k.LastUsedAt)

	u, keyID, err := st.UserByAPIKey(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, alice.ID, u.ID)
	require.Equal(t, k.ID, keyID)

	for _, bad := range []string{"", APIKeyPrefix, raw[len(APIKeyPrefix):], raw + "x", strings.ToUpper(raw)} {
		_, _, err := st.UserByAPIKey(ctx, bad)
		require.ErrorIs(t, err, ErrNotFound, bad)
	}
}

// Security: the raw key is nowhere in the database, only its hash.
func TestAPIKeyStoredHashedOnly(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := keyUser(t, st, "+27825550101")
	raw, _, err := st.CreateAPIKey(ctx, alice.ID, "k")
	require.NoError(t, err)

	rows, err := st.db.QueryContext(ctx, `SELECT * FROM api_keys`)
	require.NoError(t, err)
	defer rows.Close()
	cols, _ := rows.Columns()
	n := 0
	for rows.Next() {
		n++
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		require.NoError(t, rows.Scan(ptrs...))
		for i, v := range vals {
			require.NotContains(t, v.String, raw[len(APIKeyPrefix):], "column %s holds the raw key", cols[i])
		}
	}
	require.Equal(t, 1, n)
}

func TestRevokedKeyNoLongerAuthenticates(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := keyUser(t, st, "+27825550101")
	raw, k, err := st.CreateAPIKey(ctx, alice.ID, "k")
	require.NoError(t, err)
	require.NoError(t, st.RevokeAPIKey(ctx, alice.ID, k.ID))
	_, _, err = st.UserByAPIKey(ctx, raw)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, st.RevokeAPIKey(ctx, alice.ID, k.ID), ErrNotFound, "revoking twice is not found")
	keys, err := st.APIKeysOf(ctx, alice.ID)
	require.NoError(t, err)
	require.Empty(t, keys)
}

// Security: another user's key id behaves exactly like one that does not exist.
func TestRevokeAnotherUsersKeyIsNotFound(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := keyUser(t, st, "+27825550101")
	bob := keyUser(t, st, "+27825550102")
	raw, k, err := st.CreateAPIKey(ctx, bob.ID, "bob's")
	require.NoError(t, err)
	require.ErrorIs(t, st.RevokeAPIKey(ctx, alice.ID, k.ID), ErrNotFound)
	require.ErrorIs(t, st.RevokeAPIKey(ctx, alice.ID, "k_nope"), ErrNotFound)
	u, _, err := st.UserByAPIKey(ctx, raw)
	require.NoError(t, err, "bob's key still works")
	require.Equal(t, bob.ID, u.ID)
}

func TestAPIKeysOfListsOnlyMine(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	now := t0
	st.Now = func() time.Time { return now }
	alice := keyUser(t, st, "+27825550101")
	bob := keyUser(t, st, "+27825550102")
	_, first, err := st.CreateAPIKey(ctx, alice.ID, "first")
	require.NoError(t, err)
	now = t0.Add(time.Minute)
	_, second, err := st.CreateAPIKey(ctx, alice.ID, "second")
	require.NoError(t, err)
	_, _, err = st.CreateAPIKey(ctx, bob.ID, "bob's")
	require.NoError(t, err)

	keys, err := st.APIKeysOf(ctx, alice.ID)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, second.ID, keys[0].ID, "newest first")
	require.Equal(t, first.ID, keys[1].ID)
	for _, k := range keys {
		require.Equal(t, alice.ID, k.UserID)
	}
}

func TestKeyNameRequired(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := keyUser(t, st, "+27825550101")
	for _, bad := range []string{"", "   ", strings.Repeat("x", 61)} {
		_, _, err := st.CreateAPIKey(ctx, alice.ID, bad)
		require.ErrorIs(t, err, ErrInvalid, "%q", bad)
	}
	_, _, err := st.CreateAPIKey(ctx, alice.ID, strings.Repeat("é", 60))
	require.NoError(t, err, "60 characters, not 60 bytes")
}

func TestTouchIsThrottled(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	now := t0
	st.Now = func() time.Time { return now }
	alice := keyUser(t, st, "+27825550101")
	_, k, err := st.CreateAPIKey(ctx, alice.ID, "k")
	require.NoError(t, err)

	lastUsed := func() time.Time {
		keys, err := st.APIKeysOf(ctx, alice.ID)
		require.NoError(t, err)
		require.NotNil(t, keys[0].LastUsedAt)
		return keys[0].LastUsedAt.UTC()
	}
	require.NoError(t, st.TouchAPIKey(ctx, k.ID))
	require.Equal(t, t0, lastUsed())
	now = t0.Add(time.Minute)
	require.NoError(t, st.TouchAPIKey(ctx, k.ID))
	require.Equal(t, t0, lastUsed(), "a minute later: no write")
	now = t0.Add(6 * time.Minute)
	require.NoError(t, st.TouchAPIKey(ctx, k.ID))
	require.Equal(t, t0.Add(6*time.Minute), lastUsed(), "six minutes later: written")
}

func TestRevokeAllAPIKeys(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := keyUser(t, st, "+27825550101")
	bob := keyUser(t, st, "+27825550102")
	a1, _, _ := st.CreateAPIKey(ctx, alice.ID, "a1")
	a2, _, _ := st.CreateAPIKey(ctx, alice.ID, "a2")
	b1, _, _ := st.CreateAPIKey(ctx, bob.ID, "b1")
	require.NoError(t, st.RevokeAllAPIKeys(ctx, alice.ID))
	for _, raw := range []string{a1, a2} {
		_, _, err := st.UserByAPIKey(ctx, raw)
		require.ErrorIs(t, err, ErrNotFound)
	}
	_, _, err := st.UserByAPIKey(ctx, b1)
	require.NoError(t, err)
}

func TestKeyLimit(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := keyUser(t, st, "+27825550101")
	var last *APIKey
	for i := 0; i < MaxAPIKeysPerUser; i++ {
		_, k, err := st.CreateAPIKey(ctx, alice.ID, "k")
		require.NoError(t, err)
		last = k
	}
	_, _, err := st.CreateAPIKey(ctx, alice.ID, "one too many")
	require.ErrorIs(t, err, ErrTooManyKeys)
	require.NoError(t, st.RevokeAPIKey(ctx, alice.ID, last.ID))
	_, _, err = st.CreateAPIKey(ctx, alice.ID, "room again")
	require.NoError(t, err, "revoked keys do not count")
}

// Mechanism: the limit holds when many creates race for one user, because
// the count and the insert are a single statement.
func TestKeyLimitHoldsUnderConcurrency(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	alice := keyUser(t, st, "+27825550101")
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				_, _, err := st.CreateAPIKey(ctx, alice.ID, "racer")
				if err == nil || errors.Is(err, ErrTooManyKeys) {
					return
				}
				if !strings.Contains(err.Error(), "locked") && !strings.Contains(err.Error(), "busy") {
					t.Errorf("unexpected: %v", err)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	keys, err := st.APIKeysOf(ctx, alice.ID)
	require.NoError(t, err)
	require.Len(t, keys, MaxAPIKeysPerUser)
}
