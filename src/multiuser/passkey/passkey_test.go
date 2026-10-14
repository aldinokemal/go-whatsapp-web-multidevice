package passkey

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

func newSvc(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	s, err := New("wtf.onrender.com", "https://wtf.onrender.com", "WTF", st)
	require.NoError(t, err)
	return s, st
}

func TestBeginRegistrationAsksForADiscoverableCredential(t *testing.T) {
	s, st := newSvc(t)
	u, _ := st.CreateUser(context.Background(), "+27825550100")
	opts, id, err := s.BeginRegistration(context.Background(), u)
	require.NoError(t, err)
	require.NotEmpty(t, id)
	var parsed struct {
		PublicKey struct {
			RP                     struct{ ID string } `json:"rp"`
			AuthenticatorSelection struct {
				ResidentKey string `json:"residentKey"`
			} `json:"authenticatorSelection"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(opts, &parsed))
	require.Equal(t, "wtf.onrender.com", parsed.PublicKey.RP.ID)
	require.Equal(t, "required", parsed.PublicKey.AuthenticatorSelection.ResidentKey, "usernameless sign-in needs a resident key")
}

func TestCeremoniesAreSingleUseUserBoundAndExpire(t *testing.T) {
	s, st := newSvc(t)
	a, _ := st.CreateUser(context.Background(), "+27825550100")
	b, _ := st.CreateUser(context.Background(), "+27825550101")
	_, id, err := s.BeginRegistration(context.Background(), a)
	require.NoError(t, err)

	err = s.FinishRegistration(context.Background(), b, id, []byte(`{}`), "x")
	require.ErrorIs(t, err, ErrCeremony, "b cannot finish a's ceremony")
	err = s.FinishRegistration(context.Background(), a, id, []byte(`{}`), "x")
	require.ErrorIs(t, err, ErrCeremony, "a ceremony is consumed by its first attempt")

	_, id2, _ := s.BeginLogin(context.Background())
	s.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	_, err = s.FinishLogin(context.Background(), id2, []byte(`{}`))
	require.ErrorIs(t, err, ErrCeremony, "expired")
}
