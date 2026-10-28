package config

import (
	"net/url"
	"strings"
)

// BrowserSourceAllowed reports whether a browser-supplied source is trusted.
// A request with no Origin, no Referer, and no cross-site fetch metadata is
// treated as a non-browser client. scheme and host are the request's own
// public scheme and host, including a non-default port.
func BrowserSourceAllowed(scheme, host, origin, referer, fetchSite string) bool {
	origin = strings.TrimSpace(origin)
	referer = strings.TrimSpace(referer)
	fetchSite = strings.ToLower(strings.TrimSpace(fetchSite))
	if origin == "" && referer == "" && fetchSite != "cross-site" {
		return true
	}
	if origin != "" && !OriginAllowed(scheme, host, origin) {
		return false
	}
	if referer != "" && !refererAllowed(scheme, host, referer) {
		return false
	}
	if fetchSite == "cross-site" && origin == "" && referer == "" {
		return false
	}
	return true
}

// OriginAllowed reports whether origin matches the request host or a
// configured trusted origin. The literal "null" origin is never trusted.
func OriginAllowed(scheme, host, origin string) bool {
	normalized := NormalizeOrigin(origin)
	if normalized == "" {
		return false
	}
	for _, allowed := range TrustedBrowserOrigins(scheme, host) {
		if strings.EqualFold(normalized, allowed) {
			return true
		}
	}
	return false
}

// TrustedBrowserOrigins is the allowlist shared by the HTTP guard and the
// WebSocket upgrader. It includes the request's own scheme and host (the
// embedded dashboard), APP_TRUSTED_ORIGINS, specific CORS origins, and the
// public app URLs already configured for OAuth and Chatwoot. A CORS wildcard
// is not trusted.
func TrustedBrowserOrigins(scheme, host string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(raw string) {
		normalized := NormalizeOrigin(raw)
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	add(requestOrigin(scheme, host))
	for _, raw := range AppTrustedOrigins {
		add(raw)
	}
	for _, raw := range AppCORSAllowedOrigins {
		add(raw)
	}
	add(McpOAuthIssuerURL)
	add(ChatwootWebhookURL)
	return out
}

// NormalizeOrigin returns scheme://host[:port] for an http(s) URL or origin.
// Default ports are omitted. An empty string means the value is not a usable origin.
func NormalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" || strings.EqualFold(raw, "null") {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := parsed.Port()
	if port == "" || (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return scheme + "://" + host
	}
	return scheme + "://" + host + ":" + port
}

func requestOrigin(scheme, host string) string {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme != "http" && scheme != "https" {
		scheme = "http"
	}
	return NormalizeOrigin(scheme + "://" + strings.TrimSpace(host))
}

func refererAllowed(scheme, host, referer string) bool {
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	return OriginAllowed(scheme, host, parsed.Scheme+"://"+parsed.Host)
}
