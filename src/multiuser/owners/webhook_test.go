package owners

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/stretchr/testify/require"
)

func stubLookup(t *testing.T, m map[string][]net.IP) {
	t.Helper()
	saved := lookupIP
	t.Cleanup(func() { lookupIP = saved })
	lookupIP = func(_ context.Context, host string) ([]net.IP, error) {
		if ips, ok := m[host]; ok {
			return ips, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
}

func TestWebhookURLsMustBePublicHTTPS(t *testing.T) {
	stubLookup(t, map[string][]net.IP{
		"hooks.example.com":  {net.ParseIP("93.184.216.34")},
		"sneaky.example.com": {net.ParseIP("93.184.216.34"), net.ParseIP("10.1.2.3")},
		"meta.example.com":   {net.ParseIP("169.254.169.254")},
	})
	ctx := context.Background()
	require.NoError(t, ValidateWebhookURL(ctx, "https://hooks.example.com/in"))
	require.NoError(t, ValidateWebhookURL(ctx, ""), "empty clears the webhook")
	for _, bad := range []string{
		"http://hooks.example.com/in",       // not https
		"https://user:pw@hooks.example.com", // credentials in the URL
		"https://127.0.0.1/in",
		"https://[::1]/in",
		"https://10.0.0.5/in",
		"https://192.168.1.1/in",
		"https://100.64.0.1/in",         // carrier-grade NAT
		"https://sneaky.example.com/in", // any private answer refuses it
		"https://meta.example.com/in",
		"https://nowhere.example.com/in", // does not resolve
		"ftp://hooks.example.com/",
		"not a url",
	} {
		require.Error(t, ValidateWebhookURL(ctx, bad), bad)
	}
}

func TestScopedWebhookSettersRefuseUnsafeConfig(t *testing.T) {
	stubLookup(t, map[string][]net.IP{"hooks.example.com": {net.ParseIP("93.184.216.34")}})
	st, _, a, _ := fixture(t)
	inner := &innerStub{}
	svc := NewScopedDevices(inner, st)
	ctx := auth.WithUser(context.Background(), a)

	require.Error(t, svc.SetDeviceWebhook(ctx, "d_a1", "https://127.0.0.1/x"))
	require.Error(t, svc.SetDeviceWebhookConfig(ctx, "d_a1", &chatstorage.DeviceWebhookConfig{
		WebhookURL: strp("https://hooks.example.com/in"), WebhookInsecureSkipVerify: true}))
	_, err := svc.AddDevice(ctx, "", &chatstorage.DeviceWebhookConfig{WebhookURL: strp("https://10.0.0.1/")})
	require.Error(t, err)
	require.Empty(t, inner.touched)
	require.Empty(t, inner.added)

	require.NoError(t, svc.SetDeviceWebhookConfig(ctx, "d_a1", &chatstorage.DeviceWebhookConfig{
		WebhookURL: strp("https://hooks.example.com/in"), WebhookSecret: "s"}))
	require.NoError(t, svc.SetDeviceWebhook(ctx, "d_a1", ""))
}

// Mechanism: even a URL that passed at save time cannot reach a private
// address at delivery (DNS can change), follow a redirect, or skip TLS checks.
func TestHardenedWebhookClientRefusesPrivateAddressesAndRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()

	tr := &http.Transport{}
	tr.TLSClientConfig = nil
	client := &http.Client{Transport: tr, Timeout: 2 * time.Second}
	HardenWebhookClient(tr, client)

	_, err := client.Post(srv.URL, "application/json", nil)
	require.Error(t, err, "loopback is refused at dial time")
	require.Error(t, client.CheckRedirect(&http.Request{}, nil), "redirects are not followed")
	require.NotNil(t, tr.TLSClientConfig)
	require.False(t, tr.TLSClientConfig.InsecureSkipVerify)
	require.Nil(t, tr.Proxy, "no proxy: the dial check must see the real destination")
}

func strp(s string) *string { return &s }
