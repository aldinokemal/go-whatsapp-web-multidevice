package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

func TestAdminLinkSkipsAppInit(t *testing.T) {
	require.True(t, commandSkipsAppInit([]string{"admin-link", "+27825550100"}))
	require.False(t, commandSkipsAppInit([]string{"rest"}))
	require.False(t, commandSkipsAppInit([]string{}))
}

func TestMintAdminLink(t *testing.T) {
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	defer st.Close()
	s := multiuser.Settings{PublicURL: "https://wtf.onrender.com"}

	link, err := mintAdminLink(context.Background(), st, s, "+27 82 555 0100")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(link, "https://wtf.onrender.com/signin/verify?t="))
	u, err := st.UserByPhone(context.Background(), "+27825550100")
	require.NoError(t, err, "user created")

	_, err = mintAdminLink(context.Background(), st, s, "+27 82 555 0100")
	require.NoError(t, err, "second run reuses the user")
	users, _ := st.ListUsers(context.Background())
	require.Len(t, users, 1)
	_ = u

	_, err = mintAdminLink(context.Background(), st, s, "not a number")
	require.Error(t, err)
}

func TestMintAdminLinkTrimsTrailingSlash(t *testing.T) {
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	defer st.Close()
	link, err := mintAdminLink(context.Background(), st, multiuser.Settings{PublicURL: "https://wtf.onrender.com/"}, "+27825550100")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(link, "https://wtf.onrender.com/signin/verify?t="), link)
}

func TestMintAdminLinkRefusesDisabledUser(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	defer st.Close()
	u, err := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, err)
	require.NoError(t, st.SetUserStatus(ctx, u.ID, store.StatusDisabled))

	link, err := mintAdminLink(ctx, st, multiuser.Settings{PublicURL: "https://x"}, "+27825550100")
	require.Error(t, err)
	require.Empty(t, link)
	require.Contains(t, err.Error(), "disabled")
}

func TestAdminLinkRefusesWhenMultiUserOff(t *testing.T) {
	t.Setenv("WTF_MULTIUSER", "")
	dbPath := filepath.Join(t.TempDir(), "never.db")
	t.Setenv("WTF_DB_URI", "file:"+dbPath)
	err := adminLinkCmd.RunE(adminLinkCmd, []string{"+27825550100"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "WTF_MULTIUSER")
	require.NoFileExists(t, dbPath)
}
