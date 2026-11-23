package owners

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
)

// Webhooks on a shared server: every user may point their own numbers'
// events at a URL, so the server must never be usable to reach what only it
// can reach (localhost, the private network, the cloud metadata address).
// ValidateWebhookURL is the save-time check. The delivery-time guard (which
// also covers DNS answers that change after saving) is owned by the shared
// outbound guard (branch phase2/outbound-guard); swap publicAddr for it once
// that lands.

// lookupIP is net.DefaultResolver.LookupIP; a var so tests can stub DNS.
var lookupIP = func(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

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
	host := u.Hostname()
	if ip, err := netip.ParseAddr(host); err == nil {
		if !publicAddr(ip) {
			return errUnsafeWebhook
		}
		return nil
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := lookupIP(lctx, host)
	if err != nil || len(ips) == 0 {
		return pkgError.ValidationError("we couldn't find that webhook address")
	}
	for _, ip := range ips {
		a, ok := netip.AddrFromSlice(ip)
		if !ok || !publicAddr(a.Unmap()) {
			return errUnsafeWebhook
		}
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

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can map to private IPv4
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// publicAddr reports whether ip is an ordinary public unicast address.
func publicAddr(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
