package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPasskeys(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550101")
	require.NoError(t, st.AddPasskey(ctx, Passkey{CredentialID: "cred1", UserID: a.ID, Name: "Safari on iOS", CredentialJSON: []byte(`{}`)}))

	list, err := st.PasskeysOf(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	pk, err := st.PasskeyByCredentialID(ctx, "cred1")
	require.NoError(t, err)
	require.Equal(t, a.ID, pk.UserID)

	require.NoError(t, st.UpdatePasskeyCredential(ctx, "cred1", []byte(`{"n":1}`)))
	pk, _ = st.PasskeyByCredentialID(ctx, "cred1")
	require.JSONEq(t, `{"n":1}`, string(pk.CredentialJSON))
	require.NotNil(t, pk.LastUsedAt)

	require.ErrorIs(t, st.DeletePasskey(ctx, b.ID, "cred1"), ErrNotFound, "cannot delete another user's passkey")
	require.NoError(t, st.DeletePasskey(ctx, a.ID, "cred1"))
}
