package owners

import (
	"context"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/stretchr/testify/require"
)

// Security: a per-device webhook pointing at a non-public address is refused
// at save, as a 400, before upstream stores it. (Delivery is guarded again at
// dial time; this is the early, friendly check.)
func TestWebhookSaveRefusesNonPublicURLs(t *testing.T) {
	st, _, a, _ := fixture(t)
	inner := &innerStub{}
	svc := NewScopedDevices(inner, st)
	ctxA := auth.WithUser(context.Background(), a)

	for _, bad := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:3000/",
		"http://[::1]/",
		"http://10.0.0.5/hook",
		"file:///etc/passwd",
	} {
		err := svc.SetDeviceWebhook(ctxA, "d_a1", bad)
		var ve pkgError.ValidationError
		require.ErrorAs(t, err, &ve, "SetDeviceWebhook %s", bad)

		u := bad
		err = svc.SetDeviceWebhookConfig(ctxA, "d_a1", &chatstorage.DeviceWebhookConfig{WebhookURL: &u})
		require.ErrorAs(t, err, &ve, "SetDeviceWebhookConfig %s", bad)
	}
	require.Empty(t, inner.touched, "nothing refused reached upstream")

	c, _ := st.CreateUser(context.Background(), "+27825550109")
	bad := "http://[::ffff:127.0.0.1]/"
	_, err := svc.AddDevice(auth.WithUser(context.Background(), c), "", &chatstorage.DeviceWebhookConfig{WebhookURL: &bad})
	var ve pkgError.ValidationError
	require.ErrorAs(t, err, &ve)
	require.Empty(t, inner.added, "no device created with a refused webhook")

	// A public address, and clearing the webhook, still work.
	require.NoError(t, svc.SetDeviceWebhook(ctxA, "d_a1", "https://8.8.8.8/hook"))
	require.NoError(t, svc.SetDeviceWebhook(ctxA, "d_a1", ""))
	empty := ""
	require.NoError(t, svc.SetDeviceWebhookConfig(ctxA, "d_a1", &chatstorage.DeviceWebhookConfig{WebhookURL: &empty}))
	require.NoError(t, svc.SetDeviceWebhookConfig(ctxA, "d_a1", nil))
}
