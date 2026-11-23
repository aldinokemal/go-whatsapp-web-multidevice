package auth

import (
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

// A session's handle is stable, opaque and keyed: it is neither the stored
// hash nor derivable without the server secret.
func TestSessionHandleIsOpaqueAndKeyed(t *testing.T) {
	cfg := Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: time.Hour}
	m := New(cfg, nil)
	h := store.HashToken("raw-session")
	got := m.SessionHandle(h)
	require.Len(t, got, 32)
	require.Equal(t, got, m.SessionHandle(h))
	require.NotContains(t, h, got)
	require.NotEqual(t, got, m.SessionHandle(store.HashToken("other")))
	cfg.Secret = []byte("fedcba9876543210fedcba9876543210")
	require.NotEqual(t, got, New(cfg, nil).SessionHandle(h))
}

func TestIsSuperadminPhoneNormalises(t *testing.T) {
	m := New(Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: time.Hour,
		Superadmins: map[string]bool{"+27825550100": true}}, nil)
	require.True(t, m.IsSuperadminPhone("27 82 555 0100"))
	require.False(t, m.IsSuperadminPhone("+27825550101"))
	require.False(t, m.IsSuperadminPhone("nope"))
}
