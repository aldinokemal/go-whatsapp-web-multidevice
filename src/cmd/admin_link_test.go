package cmd

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/spf13/viper"
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
	_, err = st.UserByPhone(context.Background(), "+27825550100")
	require.NoError(t, err, "user created")
	raw := link[strings.Index(link, "t=")+2:]
	ok, err := st.LoginTokenValid(context.Background(), raw)
	require.NoError(t, err)
	require.True(t, ok, "minted token is valid")

	_, err = mintAdminLink(context.Background(), st, s, "+27 82 555 0100")
	require.NoError(t, err, "second run reuses the user")
	users, err := st.ListUsers(context.Background())
	require.NoError(t, err)
	require.Len(t, users, 1)

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

func TestMintedTokenExpiresInAboutFifteenMinutes(t *testing.T) {
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	defer st.Close()
	now := time.Now()
	st.Now = func() time.Time { return now }
	link, err := mintAdminLink(context.Background(), st, multiuser.Settings{PublicURL: "https://x"}, "+27825550100")
	require.NoError(t, err)
	raw := link[strings.Index(link, "t=")+2:]
	now = now.Add(14 * time.Minute)
	ok, _ := st.LoginTokenValid(context.Background(), raw)
	require.True(t, ok)
	now = now.Add(2 * time.Minute)
	ok, _ = st.LoginTokenValid(context.Background(), raw)
	require.False(t, ok, "expired after 15 minutes")
}

func TestMintAdminLinkAuditsSignupOnceAndNoToken(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "a.db")
	st, err := store.Open("file:" + dbPath)
	require.NoError(t, err)
	defer st.Close()
	_, err = mintAdminLink(ctx, st, multiuser.Settings{PublicURL: "https://x"}, "+27825550100")
	require.NoError(t, err)
	_, err = mintAdminLink(ctx, st, multiuser.Settings{PublicURL: "https://x"}, "+27825550100")
	require.NoError(t, err)
	n := func(a string) int { return auditCount(t, dbPath, a) }
	require.Equal(t, 1, n("signup"))
	require.Equal(t, 2, n("admin_link_minted"))
}

func TestSettingGetenvFallsBackToViper(t *testing.T) {
	const k = "WTF_PRODUCT_NAME"
	t.Setenv("WTF_MULTIUSER", "")
	viper.Set("WTF_MULTIUSER", "true")
	viper.Set(k, "FromDotEnv")
	viper.Set("SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Cleanup(func() {
		viper.Set("WTF_MULTIUSER", "")
		viper.Set(k, "")
		viper.Set("SESSION_SECRET", "")
	})
	s, err := loadMultiUserSettings()
	require.NoError(t, err)
	require.True(t, s.Enabled)
	require.Equal(t, "FromDotEnv", s.ProductName)
	t.Setenv(k, "FromEnv")
	s, _ = loadMultiUserSettings()
	require.Equal(t, "FromEnv", s.ProductName, "os env wins")
}

func TestResolveAccountsDB(t *testing.T) {
	dir := t.TempDir()
	abs, err := resolveAccountsDB("file:" + filepath.Join(dir, "accounts.db") + "?_pragma=x")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "accounts.db"), abs)
	_, err = resolveAccountsDB("file:" + filepath.Join(dir, "nope", "accounts.db"))
	require.Error(t, err)
	require.Contains(t, err.Error(), filepath.Join(dir, "nope"))
}

func adminLinkEnv(t *testing.T, supers string) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "accounts.db")
	t.Setenv("WTF_MULTIUSER", "true")
	t.Setenv("SESSION_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("WTF_DB_URI", "file:"+dbPath)
	t.Setenv("WTF_SUPERADMIN_PHONES", supers)
	t.Setenv("WTF_PUBLIC_URL", "https://x")
	t.Cleanup(func() { adminLinkForce = false })
	return dbPath
}

func usersIn(t *testing.T, dbPath string) []store.User {
	st, err := store.Open("file:" + dbPath)
	require.NoError(t, err)
	defer st.Close()
	us, err := st.ListUsers(context.Background())
	require.NoError(t, err)
	return us
}

func TestAdminLinkRefusesNonSuperadminWithoutForce(t *testing.T) {
	for _, supers := range []string{"+27825550199", ""} {
		dbPath := adminLinkEnv(t, supers)
		err := adminLinkCmd.RunE(adminLinkCmd, []string{"+27825550100"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "WTF_SUPERADMIN_PHONES")
		require.Empty(t, usersIn(t, dbPath), "no user row written")
	}
}

func TestAdminLinkForceAndAllowlist(t *testing.T) {
	dbPath := adminLinkEnv(t, "+27825550199")
	adminLinkForce = true
	require.NoError(t, adminLinkCmd.RunE(adminLinkCmd, []string{"+27825550100"}))
	require.Len(t, usersIn(t, dbPath), 1)

	dbPath = adminLinkEnv(t, "+27825550100")
	require.NoError(t, adminLinkCmd.RunE(adminLinkCmd, []string{"+27 82 555 0100"}))
	require.Len(t, usersIn(t, dbPath), 1)
}

func TestAdminLinkFailsOnMissingDBDirectory(t *testing.T) {
	adminLinkEnv(t, "+27825550100")
	bad := filepath.Join(t.TempDir(), "missing", "accounts.db")
	t.Setenv("WTF_DB_URI", "file:"+bad)
	err := adminLinkCmd.RunE(adminLinkCmd, []string{"+27825550100"})
	require.Error(t, err)
	require.Contains(t, err.Error(), filepath.Dir(bad))
	require.NoDirExists(t, filepath.Dir(bad))
}

func TestInitAppSkippedOnlyForAdminLink(t *testing.T) {
	calls := 0
	old := initAppFunc
	initAppFunc = func() { calls++ }
	t.Cleanup(func() { initAppFunc = old })
	initAppUnlessSkipped([]string{"admin-link", "+27825550100"})
	require.Equal(t, 0, calls)
	initAppUnlessSkipped([]string{"rest"})
	require.Equal(t, 1, calls)
}

func auditCount(t *testing.T, dbPath, action string) int {
	t.Helper()
	db, err := sql.Open(sqlite.DriverName, "file:"+dbPath)
	require.NoError(t, err)
	defer db.Close()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = ?`, action).Scan(&n))
	return n
}
