package web

import (
	"context"
	"regexp"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/capacity"
	"github.com/stretchr/testify/require"
)

func capacityEnv(t *testing.T, max int) *env {
	t.Helper()
	e := newEnv(t)
	e.d.Capacity = capacity.Service{Store: e.st, MaxPhones: max}
	remount(e, &devStub{})
	return e
}

var tileRE = regexp.MustCompile(`(?s)<div class="stat stat-link[^"]*">.*?href="([^"]+)".*?class="stat-value">([^<]+)<`)

// Behaviour (house rule T10, T12): Users and Waiting tiles open the list
// they count, and each figure equals that list's own count.
func TestCapacityTilesLinkToTheirLists(t *testing.T) {
	e := capacityEnv(t, 20)
	admin, adminID := signedIn(t, e, "+27825550100")
	manyUsers(t, e.st, 4)
	join(t, e.st, "+27825550111", "+27825550112")
	ctx := context.Background()
	require.NoError(t, e.st.SetOwner(ctx, "d_a", adminID))

	body := get(t, e, admin, "/admin/settings")
	tiles := map[string]string{}
	for _, m := range tileRE.FindAllStringSubmatch(body, -1) {
		tiles[m[1]] = m[2]
	}
	require.Equal(t, map[string]string{"/admin/users": "5", "/admin/waitlist": "2"}, tiles)
	require.Contains(t, get(t, e, admin, "/admin/users"), "5 users")
	require.Contains(t, get(t, e, admin, "/admin/waitlist"), "2 waiting")
	require.Regexp(t, `(?s)Numbers linked.*?class="stat-value">1<.*?of 20`, body)
}

// Behaviour: the linked tile turns to a warning at 90% of the cap.
func TestCapacityWarnsNearFull(t *testing.T) {
	e := capacityEnv(t, 10)
	admin, adminID := signedIn(t, e, "+27825550100")
	require.NoError(t, e.st.SetMaxDevices(context.Background(), adminID, 20))
	for i := 0; i < 8; i++ {
		require.NoError(t, e.st.SetOwner(context.Background(), "d_"+string(rune('a'+i)), adminID))
	}
	require.NotContains(t, get(t, e, admin, "/admin/settings"), "stat-warn")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_z", adminID))
	body := get(t, e, admin, "/admin/settings")
	require.Contains(t, body, "stat-warn")
	require.Contains(t, body, "Almost full")
}

// Behaviour: with no cap the tile says how many are linked, with no "of".
func TestCapacityWithoutACap(t *testing.T) {
	e := capacityEnv(t, 0)
	admin, adminID := signedIn(t, e, "+27825550100")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_a", adminID))
	body := get(t, e, admin, "/admin/settings")
	require.Regexp(t, `(?s)Numbers linked.*?class="stat-value">1<`, body)
	require.NotRegexp(t, `of \d+ allowed|stat-of`, body)
	require.Contains(t, body, "No limit set")
}
