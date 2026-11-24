package owners

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/stretchr/testify/require"
)

func TestWebhookURLsMustBePublicHTTPS(t *testing.T) {
	// Which addresses are public (and hostnames that resolve to a mix) is
	// netguard's job and tested there; here: the https-only and no-userinfo
	// rules this save check adds, on literal addresses so no DNS is needed.
	ctx := context.Background()
	require.NoError(t, ValidateWebhookURL(ctx, "https://93.184.216.34/in"))
	require.NoError(t, ValidateWebhookURL(ctx, ""), "empty clears the webhook")
	for _, bad := range []string{
		"http://93.184.216.34/in",        // not https
		"https://user:pw@93.184.216.34",  // credentials in the URL
		"https://169.254.169.254/latest", // cloud metadata
		"https://127.0.0.1/in",
		"https://[::1]/in",
		"https://10.0.0.5/in",
		"https://192.168.1.1/in",
		"https://100.64.0.1/in",      // carrier-grade NAT
		"https://nowhere.invalid/in", // does not resolve (RFC 6761)
		"ftp://93.184.216.34/",
		"not a url",
	} {
		require.Error(t, ValidateWebhookURL(ctx, bad), bad)
	}
}

func TestScopedWebhookSettersRefuseUnsafeConfig(t *testing.T) {
	st, _, a, _ := fixture(t)
	inner := &innerStub{}
	svc := NewScopedDevices(inner, st)
	ctx := auth.WithUser(context.Background(), a)

	require.Error(t, svc.SetDeviceWebhook(ctx, "d_a1", "https://127.0.0.1/x"))
	require.Error(t, svc.SetDeviceWebhookConfig(ctx, "d_a1", &chatstorage.DeviceWebhookConfig{
		WebhookURL: strp("https://93.184.216.34/in"), WebhookInsecureSkipVerify: true}))
	_, err := svc.AddDevice(ctx, "", &chatstorage.DeviceWebhookConfig{WebhookURL: strp("https://10.0.0.1/")})
	require.Error(t, err)
	require.Empty(t, inner.touched)
	require.Empty(t, inner.added)

	require.NoError(t, svc.SetDeviceWebhookConfig(ctx, "d_a1", &chatstorage.DeviceWebhookConfig{
		WebhookURL: strp("https://93.184.216.34/in"), WebhookSecret: "s"}))
	require.NoError(t, svc.SetDeviceWebhook(ctx, "d_a1", ""))
}

func strp(s string) *string { return &s }
