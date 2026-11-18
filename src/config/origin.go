package config

import (
	"net/url"
	"strings"
)

// BrowserSourceAllowed reports whether a browser-supplied source is trusted.
// A request with no Origin and no Referer is treated as a non-browser client
// unless Sec-Fetch-Site marks it as a same-site or cross-site browser request.
// host is the request's own public host, including a non-default port.
func BrowserSourceAllowed(host, origin, referer, fetchSite string) bool {
	origin = strings.TrimSpace(origin)
	referer = strings.TrimSpace(referer)
	fetchSite = strings.ToLower(strings.TrimSpace(fetchSite))
	if origin == "" && referer == "" {
		return fetchSite == "" || fetchSite == "same-origin" || fetchSite == "none"
	}
	if origin != "" && !OriginAllowed(host, origin) {
		return false
	}
	if referer != "" && !refererAllowed(host, referer) {
		return false
	}
	return true
}

// OriginAllowed reports whether origin matches the request host or a
// configured trusted origin. The literal "null" origin is never trusted.
func OriginAllowed(host, origin string) bool {
	normalized := NormalizeOrigin(origin)
	if normalized == "" {
		return false
	}
	for _, allowed := range TrustedBrowserOrigins(host) {
		if strings.EqualFold(normalized, allowed) {
			return true
		}
	}
	return false
}

// TrustedBrowserOrigins is the allowlist shared by the HTTP guard and the
// WebSocket upgrader. It includes the request's own host (the embedded
// dashboard), APP_TRUSTED_ORIGINS, specific CORS origins, and the public app
// URLs already configured for OAuth and Chatwoot. A CORS wildcard is not
// trusted.
func TrustedBrowserOrigins(host string) []string {
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
	// The request's own host is trusted under either scheme, like Go's
	// CrossOriginProtection: a TLS-terminating proxy forwards https traffic
	// over plain http.
	host = strings.TrimSpace(host)
	add("https://" + host)
	add("http://" + host)
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
// Default ports are omitted. An app or extension origin such as
// capacitor://localhost is kept as written, lower-cased and without a
// trailing slash. An empty string means the value is not a usable origin.
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
		return strings.TrimRight(strings.ToLower(raw), "/")
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

func refererAllowed(host, referer string) bool {
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	return OriginAllowed(host, parsed.Scheme+"://"+parsed.Host)
}
