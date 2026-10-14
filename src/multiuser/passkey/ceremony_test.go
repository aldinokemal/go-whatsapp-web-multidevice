package passkey

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/require"
)

// enrol runs a full registration ceremony for u with a fresh software authenticator.
func enrol(t *testing.T, s *Service, u *store.User) *softAuth {
	t.Helper()
	ctx := context.Background()
	a := newSoftAuth(t)
	opts, id, err := s.BeginRegistration(ctx, u)
	require.NoError(t, err)
	require.NoError(t, s.FinishRegistration(ctx, u, id, a.register(t, opts), "Test key"))
	return a
}

func signIn(t *testing.T, s *Service, a *softAuth, handle []byte) (*store.User, error) {
	t.Helper()
	opts, id, err := s.BeginLogin(context.Background())
	require.NoError(t, err)
	return s.FinishLogin(context.Background(), id, a.login(t, opts, handle))
}

func storedCred(t *testing.T, st *store.Store, a *softAuth) (*store.Passkey, webauthn.Credential) {
	t.Helper()
	pk, err := st.PasskeyByCredentialID(context.Background(), base64.RawURLEncoding.EncodeToString(a.credID))
	require.NoError(t, err)
	var c webauthn.Credential
	require.NoError(t, json.Unmarshal(pk.CredentialJSON, &c))
	return pk, c
}

func TestRegisterThenSignInWithoutAUsername(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	a := enrol(t, s, u)

	list, err := st.PasskeysOf(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "Test key", list[0].Name)
	require.Equal(t, u.ID, string(a.userID), "user handle is the user id")

	a.count = 7
	got, err := signIn(t, s, a, nil)
	require.NoError(t, err)
	require.Equal(t, u.ID, got.ID)

	pk, c := storedCred(t, st, a)
	require.EqualValues(t, 7, c.Authenticator.SignCount, "sign count is saved after each use")
	require.NotNil(t, pk.LastUsedAt)
}

func TestDisabledUserCannotSignInWithAPasskey(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	a := enrol(t, s, u)
	require.NoError(t, st.SetUserStatus(ctx, u.ID, store.StatusDisabled))

	a.count = 1
	got, err := signIn(t, s, a, nil)
	require.ErrorIs(t, err, ErrDisabled)
	require.Nil(t, got)
}

func TestDisabledUserCannotRegisterAPasskey(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	opts, id, err := s.BeginRegistration(ctx, u)
	require.NoError(t, err)
	require.NoError(t, st.SetUserStatus(ctx, u.ID, store.StatusDisabled))
	err = s.FinishRegistration(ctx, u, id, newSoftAuth(t).register(t, opts), "k")
	require.ErrorIs(t, err, ErrDisabled)
	_, _, err = s.BeginRegistration(ctx, u)
	require.ErrorIs(t, err, ErrDisabled)
}

func TestAReplayedAssertionIsRefused(t *testing.T) {
	s, st := newSvc(t)
	u, _ := st.CreateUser(context.Background(), "+27825550100")
	a := enrol(t, s, u)
	a.count = 1

	opts, id, _ := s.BeginLogin(context.Background())
	body := a.login(t, opts, nil)
	_, err := s.FinishLogin(context.Background(), id, body)
	require.NoError(t, err)
	_, err = s.FinishLogin(context.Background(), id, body)
	require.ErrorIs(t, err, ErrCeremony, "same ceremony twice")

	_, id2, _ := s.BeginLogin(context.Background())
	_, err = s.FinishLogin(context.Background(), id2, body)
	require.ErrorIs(t, err, ErrInvalid, "old assertion against a new challenge")
}

func TestCeremoniesAreBoundToTheirFlow(t *testing.T) {
	s, st := newSvc(t)
	u, _ := st.CreateUser(context.Background(), "+27825550100")
	_, loginID, _ := s.BeginLogin(context.Background())
	require.ErrorIs(t, s.FinishRegistration(context.Background(), u, loginID, []byte(`{}`), "x"), ErrCeremony)

	_, regID, _ := s.BeginRegistration(context.Background(), u)
	_, err := s.FinishLogin(context.Background(), regID, []byte(`{}`))
	require.ErrorIs(t, err, ErrCeremony)

	_, err = s.FinishLogin(context.Background(), "", []byte(`{}`))
	require.ErrorIs(t, err, ErrCeremony)
	_, err = s.FinishLogin(context.Background(), "made-up", []byte(`{}`))
	require.ErrorIs(t, err, ErrCeremony)
}

func TestAWrongOriginIsRefusedAndTheErrorLeaksNothing(t *testing.T) {
	s, st := newSvc(t)
	u, _ := st.CreateUser(context.Background(), "+27825550100")
	a := enrol(t, s, u)
	a.origin = "https://evil.example"
	a.count = 1
	opts, id, _ := s.BeginLogin(context.Background())
	_, err := s.FinishLogin(context.Background(), id, a.login(t, opts, nil))
	require.ErrorIs(t, err, ErrInvalid)
	require.NotContains(t, err.Error(), challengeOf(t, opts))
	require.NotContains(t, err.Error(), "evil.example")

	b := newSoftAuth(t)
	b.origin = "https://evil.example"
	opts, id, _ = s.BeginRegistration(context.Background(), u)
	err = s.FinishRegistration(context.Background(), u, id, b.register(t, opts), "x")
	require.ErrorIs(t, err, ErrInvalid)
	require.NotContains(t, err.Error(), challengeOf(t, opts))
}

func TestAForgedUserHandleCannotSignInAsSomeoneElse(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	alice, _ := st.CreateUser(ctx, "+27825550100")
	bob, _ := st.CreateUser(ctx, "+27825550101")
	enrol(t, s, bob)
	a := enrol(t, s, alice)
	a.count = 1
	got, err := signIn(t, s, a, []byte(bob.ID))
	require.ErrorIs(t, err, ErrInvalid)
	require.Nil(t, got)
}

func TestUnknownOrDeletedPasskeyIsRefused(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	a := enrol(t, s, u)
	require.NoError(t, st.DeletePasskey(ctx, u.ID, base64.RawURLEncoding.EncodeToString(a.credID)))
	a.count = 1
	_, err := signIn(t, s, a, nil)
	require.ErrorIs(t, err, ErrInvalid)

	stranger := newSoftAuth(t)
	stranger.userID = []byte(u.ID)
	_, err = signIn(t, s, stranger, nil)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestUserVerificationIsRequired(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")

	noUV := newSoftAuth(t)
	noUV.flags = flagUP
	opts, id, _ := s.BeginRegistration(ctx, u)
	require.ErrorIs(t, s.FinishRegistration(ctx, u, id, noUV.register(t, opts), "x"), ErrInvalid)

	a := enrol(t, s, u)
	a.flags = flagUP
	a.count = 1
	_, err := signIn(t, s, a, nil)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestACloneWarningRefusesSignInAndKeepsTheCounter(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	a := enrol(t, s, u)
	a.count = 5
	_, err := signIn(t, s, a, nil)
	require.NoError(t, err)

	a.count = 3 // a copy of the key that is behind the real one
	_, err = signIn(t, s, a, nil)
	require.ErrorIs(t, err, ErrInvalid)
	_, c := storedCred(t, st, a)
	require.EqualValues(t, 5, c.Authenticator.SignCount)
}

func TestTheSameAuthenticatorCannotRegisterTwice(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	a := enrol(t, s, u)
	opts, id, err := s.BeginRegistration(ctx, u)
	require.NoError(t, err)
	var o struct {
		PublicKey struct {
			Exclude []struct{ ID string } `json:"excludeCredentials"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(opts, &o))
	require.Len(t, o.PublicKey.Exclude, 1, "existing passkeys are excluded")
	require.Error(t, s.FinishRegistration(ctx, u, id, a.register(t, opts), "again"))
	list, _ := st.PasskeysOf(ctx, u.ID)
	require.Len(t, list, 1)
}

func TestPasskeyNamesAreTrimmedAndCapped(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	a := newSoftAuth(t)
	opts, id, _ := s.BeginRegistration(ctx, u)
	require.NoError(t, s.FinishRegistration(ctx, u, id, a.register(t, opts), "  "+strings.Repeat("é", 200)+"\n"))
	list, _ := st.PasskeysOf(ctx, u.ID)
	require.Equal(t, strings.Repeat("é", maxNameRunes), list[0].Name)

	b := newSoftAuth(t)
	opts, id, _ = s.BeginRegistration(ctx, u)
	require.NoError(t, s.FinishRegistration(ctx, u, id, b.register(t, opts), "   "))
	list, _ = st.PasskeysOf(ctx, u.ID)
	require.Equal(t, "Passkey", list[1].Name)
}

func TestTheCeremonyStoreIsBoundedAndSwept(t *testing.T) {
	s, _ := newSvc(t)
	s.max = 3
	for i := 0; i < 3; i++ {
		_, _, err := s.BeginLogin(context.Background())
		require.NoError(t, err)
	}
	_, _, err := s.BeginLogin(context.Background())
	require.ErrorIs(t, err, ErrBusy)

	later := time.Now().Add(ceremonyTTL + time.Second)
	s.now = func() time.Time { return later }
	_, _, err = s.BeginLogin(context.Background())
	require.NoError(t, err, "expired ceremonies are swept to make room")
	require.Equal(t, 1, s.pending())
}

func TestANewRegistrationReplacesTheUsersPendingOne(t *testing.T) {
	s, st := newSvc(t)
	ctx := context.Background()
	u, _ := st.CreateUser(ctx, "+27825550100")
	for i := 0; i < 5; i++ {
		_, _, err := s.BeginRegistration(ctx, u)
		require.NoError(t, err)
	}
	require.Equal(t, 1, s.pending(), "one user cannot fill the store")
}

func TestTheCeremonyStoreIsSafeForConcurrentUse(t *testing.T) {
	s, _ := newSvc(t)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, id, err := s.BeginLogin(context.Background())
			if err != nil {
				errs <- err
				return
			}
			if _, err := s.FinishLogin(context.Background(), id, []byte(`{}`)); !errors.Is(err, ErrInvalid) {
				errs <- fmt.Errorf("want ErrInvalid, got %v", err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	require.Equal(t, 0, s.pending())
}

func TestNewRefusesAnOriginOutsideTheRPID(t *testing.T) {
	st, _ := store.Open("file:" + t.TempDir() + "/a.db")
	defer st.Close()
	for _, c := range []struct{ rp, origin string }{
		{"wtf.onrender.com", "https://evil.example"},
		{"wtf.onrender.com", "http://wtf.onrender.com"},
		{"onrender.com", "https://wtf.onrender.com.evil.example"},
		{"wtf.onrender.com", "https://wtf.onrender.com/path"},
		{"", "https://wtf.onrender.com"},
	} {
		_, err := New(c.rp, c.origin, "WTF", st)
		require.Error(t, err, "%s / %s", c.rp, c.origin)
	}
	for _, c := range []struct{ rp, origin string }{
		{"wtf.onrender.com", "https://wtf.onrender.com"},
		{"onrender.com", "https://wtf.onrender.com"},
		{"localhost", "http://localhost:3000"},
	} {
		_, err := New(c.rp, c.origin, "WTF", st)
		require.NoError(t, err, "%s / %s", c.rp, c.origin)
	}
}

func TestFromPublicURL(t *testing.T) {
	rp, origin, err := FromPublicURL("https://WTF.onrender.com/")
	require.NoError(t, err)
	require.Equal(t, "wtf.onrender.com", rp)
	require.Equal(t, "https://wtf.onrender.com", origin)
	rp, origin, err = FromPublicURL("http://localhost:3000")
	require.NoError(t, err)
	require.Equal(t, "localhost", rp)
	require.Equal(t, "http://localhost:3000", origin)
	_, _, err = FromPublicURL("not a url")
	require.Error(t, err)
}

// The register/login kind is checked on its own, not only through the user
// binding (a login ceremony carries no user, a registration always does).
func TestTakeChecksTheFlowKind(t *testing.T) {
	s, _ := newSvc(t)
	_, id, err := s.BeginLogin(context.Background())
	require.NoError(t, err)
	_, err = s.take(id, kindRegister, "")
	require.ErrorIs(t, err, ErrCeremony)
	require.Equal(t, 0, s.pending(), "a wrong-flow attempt still consumes the ceremony")
}
