package owners

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/netguard"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
)

// Webhooks on a shared server: every user may point their own numbers'
// events at a URL, so the server must never be usable to reach what only it
// can reach (localhost, the private network, the cloud metadata address).
// ValidateWebhookURL is the one save-time check, used by every save path
// (AddDevice, SetDeviceWebhook, SetDeviceWebhookConfig and, through those, the
// web dialog). Which addresses count as public is decided by netguard, the same
// package whose dialer guards delivery (and so also catches DNS answers that
// change after saving).

const maxWebhookURLLen = 2048

var errUnsafeWebhook = pkgError.ValidationError("use a public https:// address for the webhook")

// ValidateWebhookURL accepts "" (no webhook) or a public https URL without
// credentials whose host resolves only to public addresses.
func ValidateWebhookURL(ctx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) > maxWebhookURLLen {
		return errUnsafeWebhook
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil || u.Hostname() == "" || u.Opaque != "" {
		return errUnsafeWebhook
	}
	if err := netguard.CheckURLContext(ctx, raw); err != nil {
		if errors.Is(err, netguard.ErrUnresolved) {
			return pkgError.ValidationError("we couldn't find that webhook address")
		}
		return errUnsafeWebhook
	}
	return nil
}

// validateWebhookConfig checks every URL in cfg and refuses skipping TLS checks.
func validateWebhookConfig(ctx context.Context, cfg *chatstorage.DeviceWebhookConfig) error {
	if cfg == nil {
		return nil
	}
	if cfg.WebhookInsecureSkipVerify {
		return pkgError.ValidationError("webhooks must verify TLS certificates on this server")
	}
	if cfg.WebhookURL == nil {
		return nil
	}
	return ValidateWebhookURL(ctx, *cfg.WebhookURL)
}
