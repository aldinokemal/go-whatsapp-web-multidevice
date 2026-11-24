package web

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/oauth"
	"github.com/stretchr/testify/require"
)

// RevokeSubject makes fakeGrants a full GrantStore: every grant of the
// subject goes.
func (f *fakeGrants) RevokeSubject(_ context.Context, subject string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, g := range f.grants[subject] {
		f.revoked = append(f.revoked, g.FamilyID)
	}
	delete(f.grants, subject)
	return nil
}

// credentialFixture is a member with one API key and one connected app, and
// a bystander with the same, so a test can see only the target lose theirs.
func credentialFixture(t *testing.T) (e *env, g *fakeGrants, admin *http.Cookie, targetID, targetKey, otherKey string) {
	t.Helper()
	g = &fakeGrants{grants: map[string][]oauth.Grant{}}
	e = newEnv(t)
	e.d.Grants, e.d.MCPURL = g, "https://wtf.onrender.com/mcp"
	remount(e, &devStub{})
	adminCk, _ := signedIn(t, e, "+27825550100")
	_, targetID = signedIn(t, e, "+27825550101")
	_, otherID := signedIn(t, e, "+27825550102")
	ctx := context.Background()
	var err error
	targetKey, _, err = e.st.CreateAPIKey(ctx, targetID, "script")
	require.NoError(t, err)
	otherKey, _, err = e.st.CreateAPIKey(ctx, otherID, "script")
	require.NoError(t, err)
	g.grants[targetID] = []oauth.Grant{{FamilyID: "fam_target"}}
	g.grants[otherID] = []oauth.Grant{{FamilyID: "fam_other"}}
	return e, g, adminCk, targetID, targetKey, otherKey
}

// Security (a default the captain set): disabling someone revokes every API
// key and connected app they hold, so enabling them again never brings old
// credentials back to life.
func TestDisablingRevokesKeysAndConnectedApps(t *testing.T) {
	e, g, admin, targetID, targetKey, otherKey := credentialFixture(t)
	tok := csrfFor(t, e, admin)
	ctx := context.Background()

	resp, _ := postForm(t, e, admin, "/admin/users/"+targetID+"/status", url.Values{"csrf": {tok}, "status": {"disabled"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	resp, _ = postForm(t, e, admin, "/admin/users/"+targetID+"/status", url.Values{"csrf": {tok}, "status": {"active"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	_, _, err := e.st.UserByAPIKey(ctx, targetKey)
	require.Error(t, err, "the key stays dead after re-enabling")
	_, _, err = e.st.UserByAPIKey(ctx, otherKey)
	require.NoError(t, err, "nobody else's key is touched")
	require.Empty(t, g.grants[targetID], "their connected apps are gone")
	require.Equal(t, []string{"fam_target"}, g.revoked)
	require.Len(t, g.grants, 1, "the other user's app is kept")
}

// Security: changing someone's number does the same, since the number is who
// they are.
func TestChangingANumberRevokesKeysAndConnectedApps(t *testing.T) {
	e, g, admin, targetID, targetKey, otherKey := credentialFixture(t)
	tok := csrfFor(t, e, admin)
	ctx := context.Background()

	resp, _ := postForm(t, e, admin, "/admin/users/"+targetID+"/phone", url.Values{"csrf": {tok}, "phone": {"+27825550109"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	u, err := e.st.UserByID(ctx, targetID)
	require.NoError(t, err)
	require.Equal(t, "+27825550109", u.Phone)

	_, _, err = e.st.UserByAPIKey(ctx, targetKey)
	require.Error(t, err)
	_, _, err = e.st.UserByAPIKey(ctx, otherKey)
	require.NoError(t, err)
	require.Equal(t, []string{"fam_target"}, g.revoked)
}

// Behaviour: with MCP off (no grant store) disabling still revokes keys.
func TestDisablingWithoutMCPStillRevokesKeys(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, targetID := signedIn(t, e, "+27825550101")
	key, _, err := e.st.CreateAPIKey(context.Background(), targetID, "script")
	require.NoError(t, err)
	resp, _ := postForm(t, e, admin, "/admin/users/"+targetID+"/status", url.Values{"csrf": {csrfFor(t, e, admin)}, "status": {"disabled"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.NoError(t, e.st.SetUserStatus(context.Background(), targetID, store.StatusActive))
	_, _, err = e.st.UserByAPIKey(context.Background(), key)
	require.Error(t, err)
}
