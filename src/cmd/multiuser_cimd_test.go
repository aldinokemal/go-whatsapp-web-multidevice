package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	uimcp "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp"
	"github.com/stretchr/testify/require"
)

// paddedCIMDClient serves the test client's document padded with spaces to
// size bytes, and counts the fetches.
func paddedCIMDClient(t *testing.T, size int) (*http.Client, *atomic.Int64) {
	t.Helper()
	doc, err := json.Marshal(map[string]any{"client_id": testCIMDClientID, "client_name": "Claude",
		"redirect_uris": []string{testCallback}, "token_endpoint_auth_method": "none"})
	require.NoError(t, err)
	if pad := size - len(doc); pad > 0 {
		doc = append(doc, []byte(strings.Repeat(" ", pad))...)
	}
	var fetches atomic.Int64
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(doc)
	}))
	t.Cleanup(srv.Close)
	tr := srv.Client().Transport.(*http.Transport).Clone()
	addr := srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: tr, Timeout: 5 * time.Second}, &fetches
}

func authorizeStatus(t *testing.T, size int) int {
	t.Helper()
	client, _ := paddedCIMDClient(t, size)
	app, rt, _ := newMultiUserAppWith(t, uimcp.Deps{}, func(s *multiuser.Settings) {
		s.MCPClientHosts = []string{"example.com"}
		s.CIMDHTTPClient = client
	})
	ctx := context.Background()
	alice, err := rt.Store.CreateUser(ctx, "+27825550101")
	require.NoError(t, err)
	raw, _ := rt.Store.CreateSession(ctx, alice.ID, "", time.Hour)
	sum := sha256.Sum256([]byte(strings.Repeat("z", 43)))
	q := url.Values{"response_type": {"code"}, "client_id": {testCIMDClientID}, "redirect_uri": {testCallback},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"resource": {muTestPublicURL + "/mcp"}, "scope": {"mcp"}, "state": {"s"}}
	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	req.AddCookie(&http.Cookie{Name: rt.Auth.CookieName(), Value: raw})
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp.StatusCode
}

// Behaviour: a client document is read up to 64 KiB, the one cap the docs
// state and the outbound guard enforces; past it the connect is refused.
func TestClientDocumentsUpTo64KiBAreRead(t *testing.T) {
	require.Equal(t, int64(64<<10), multiuser.CIMDMaxBytes)
	require.Equal(t, http.StatusOK, authorizeStatus(t, 40<<10), "40 KiB: over the old 16 KiB default, within the cap")
	require.Equal(t, http.StatusOK, authorizeStatus(t, 64<<10), "exactly 64 KiB")
	require.Equal(t, http.StatusBadRequest, authorizeStatus(t, 64<<10+1), "one byte over")
}
