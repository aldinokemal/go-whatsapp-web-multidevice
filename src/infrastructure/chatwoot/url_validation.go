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

// ValidateChatwootURL canonicalizes the URL and then rejects it on SSRF grounds:
// no http(s) scheme, embedded credentials, the literal host "localhost", or a
// host that is an IP literal in a private/loopback/link-local/unspecified range
// (which also covers the cloud metadata address 169.254.169.254). When
// config.ChatwootAllowedHosts is non-empty, the host must additionally appear in
// that allowlist. Hostnames are NOT resolved to IPs here (DNS rebinding is out
// of scope for v1); the allowlist is the strong control for untrusted callers.
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
	if host == "localhost" {
		return fmt.Errorf("chatwoot url host %q is not allowed", host)
	}

	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("chatwoot url host %q resolves to a disallowed (private/loopback/link-local) address", host)
		}
	}

	if len(config.ChatwootAllowedHosts) > 0 {
		allowed := false
		for _, h := range config.ChatwootAllowedHosts {
			if strings.EqualFold(strings.TrimSpace(h), host) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("chatwoot url host %q is not in the allowed hosts list", host)
		}
	}
	return nil
}
