package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoginTokenSingleUseAndExpiry(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }

	raw, err := st.CreateLoginToken(ctx, "+27825550100", 15*time.Minute)
	require.NoError(t, err)

	var stored string
	require.NoError(t, st.db.QueryRow(`SELECT token_hash FROM login_tokens`).Scan(&stored))
	require.NotEqual(t, raw, stored, "only the hash is stored")

	ok, err := st.LoginTokenValid(ctx, raw)
	require.NoError(t, err)
	require.True(t, ok)
	ok, _ = st.LoginTokenValid(ctx, raw) // peeking twice never consumes
	require.True(t, ok)

	phone, err := st.ConsumeLoginToken(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, "+27825550100", phone)

	_, err = st.ConsumeLoginToken(ctx, raw)
	require.ErrorIs(t, err, ErrNotFound, "second use refused")

	late, _ := st.CreateLoginToken(ctx, "+27825550100", 15*time.Minute)
	now = now.Add(16 * time.Minute)
	ok, _ = st.LoginTokenValid(ctx, late)
	require.False(t, ok)
	_, err = st.ConsumeLoginToken(ctx, late)
	require.ErrorIs(t, err, ErrNotFound, "expired refused")

	_, err = st.ConsumeLoginToken(ctx, "nonsense")
	require.ErrorIs(t, err, ErrNotFound)
}
