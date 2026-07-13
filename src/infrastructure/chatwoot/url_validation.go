package chatwoot

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
)

// maxChatwootURLLen caps the stored Chatwoot URL length.
const maxChatwootURLLen = 512

// lookupIP resolves a hostname to its IP addresses. It is a package var so tests
// can stub DNS without network access.
var lookupIP = net.LookupIP

// CanonicalizeChatwootURL normalizes a Chatwoot base URL into a stable form so
// the UNIQUE(chatwoot_url, account_id, inbox_id) constraint treats equivalent
// URLs as equal. It lowercases the scheme and host, drops the default port,
// trims a trailing slash, and discards any query/fragment. It returns an error
// for inputs that are not usable http(s) base URLs (so validation and storage
// share one definition of "valid").
func CanonicalizeChatwootURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("chatwoot url is required")
	}
	if len(raw) > maxChatwootURLLen {
		return "", fmt.Errorf("chatwoot url exceeds %d characters", maxChatwootURLLen)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid chatwoot url: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("chatwoot url scheme must be http or https, got %q", u.Scheme)
	}
	if u.User != nil {
		return "", fmt.Errorf("chatwoot url must not contain embedded credentials")
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("chatwoot url must include a host")
	}

	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	hostPort := host
	if port != "" {
		hostPort = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		// IPv6 literal without a port still needs bracketing.
		hostPort = "[" + host + "]"
	}

	path := strings.TrimRight(u.EscapedPath(), "/")
	return scheme + "://" + hostPort + path, nil
}

// chatwootHostAllowlisted reports whether host is in the configured allowlist.
// An empty allowlist means "no allowlist configured" (returns false).
func chatwootHostAllowlisted(host string) bool {
	for _, h := range config.ChatwootAllowedHosts {
		if strings.EqualFold(strings.TrimSpace(h), host) {
			return true
		}
	}
	return false
}

// isDisallowedSSRFIP reports whether an IP is one we refuse to let a per-device
// Chatwoot client talk to: loopback, RFC1918/ULA private, link-local (which
// includes the cloud metadata address 169.254.169.254), or the unspecified
// address.
func isDisallowedSSRFIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// ValidateChatwootURL canonicalizes the URL and rejects it on SSRF grounds.
//
// When config.ChatwootAllowedHosts is set, the host MUST match the allowlist —
// and a matching host is trusted (the operator's explicit escape hatch for a
// self-hosted Chatwoot on a private network).
//
// Otherwise the host is resolved (literal IP or via DNS) and rejected if ANY
// resulting address is loopback/private/link-local/metadata/unspecified, and the
// literal name "localhost" is rejected outright. DNS rebinding (a host that
// flips to an internal IP after this check) is additionally caught at connect
// time by the guarded transport on per-device clients (see ssrfGuardedHTTPClient).
func ValidateChatwootURL(raw string) error {
	canonical, err := CanonicalizeChatwootURL(raw)
	if err != nil {
		return err
	}
	u, err := url.Parse(canonical)
	if err != nil {
		return fmt.Errorf("invalid chatwoot url: %w", err)
	}
	host := strings.ToLower(u.Hostname())

	if len(config.ChatwootAllowedHosts) > 0 {
		if chatwootHostAllowlisted(host) {
			return nil
		}
		return fmt.Errorf("chatwoot url host %q is not in the allowed hosts list", host)
	}

	if host == "localhost" {
		return fmt.Errorf("chatwoot url host %q is not allowed", host)
	}

	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		resolved, err := lookupIP(host)
		if err != nil {
			return fmt.Errorf("chatwoot url host %q could not be resolved: %w", host, err)
		}
		ips = resolved
	}
	for _, ip := range ips {
		if isDisallowedSSRFIP(ip) {
			return fmt.Errorf("chatwoot url host %q resolves to a disallowed (private/loopback/link-local) address", host)
		}
	}
	return nil
}
