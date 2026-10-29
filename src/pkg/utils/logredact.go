package utils

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

const redactedSecret = "***"

// sensitiveKeySubstrings mark a setting as secret from its name alone.
// "webhook" is intentionally absent: destination URLs are useful at startup,
// and only webhook signing secrets (keys that also match "secret" or "token")
// are fully redacted. "proxy" is handled separately because a proxy without
// userinfo is safe to print. "basic_auth" stands in for a bare "auth", which
// would also hide the non-secret mcp_oauth_* flags and URLs.
var sensitiveKeySubstrings = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"api_key",
	"apikey",
	"authorization",
	"basic_auth",
	"credential",
	"private",
}

// urlScheme finds where each URL in a flat env value such as a comma-separated
// webhook list starts.
var urlScheme = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://`)

// queryParamName matches a query separator and parameter name up to its '='.
var queryParamName = regexp.MustCompile(`[?&]([^?&=#\s]*)=`)

// secretQueryName matches a query parameter name that looks secret.
var secretQueryName = regexp.MustCompile(`(?i)secret|token|password|passwd|key|sig|auth|credential`)

// RedactSettings returns a deep copy of a Viper AllSettings map safe to print
// at startup. Key names are preserved. Secret values become "***". Other
// URL-like values keep their host and path, with userinfo and secret query
// values replaced by "***".
func RedactSettings(settings map[string]any) map[string]any {
	if settings == nil {
		return nil
	}
	return redactMap("", settings)
}

func redactMap(prefix string, settings map[string]any) map[string]any {
	out := make(map[string]any, len(settings))
	for key, value := range settings {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		out[key] = redactSettingValue(path, value)
	}
	return out
}

func redactSettingValue(key string, value any) any {
	// An empty secret stays empty so a missing setting is still visible.
	if value != "" && isFullySensitiveKey(key) {
		return redactedSecret
	}
	switch typed := value.(type) {
	case map[string]any:
		return redactMap(key, typed)
	case string:
		return redactStringSetting(key, typed)
	case []string:
		out := make([]string, len(typed))
		for i, item := range typed {
			out[i] = redactStringSetting(key, item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactSettingValue(key, item)
		}
		return out
	default:
		return value
	}
}

func redactStringSetting(key, value string) string {
	if isProxyKey(key) && proxyValueMayContainUserinfo(value) {
		return redactedSecret
	}
	return redactURLSecretsInText(value)
}

func normalizeSettingKey(key string) string {
	k := strings.ToLower(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, "-", "_")
	k = strings.ReplaceAll(k, ".", "_")
	k = strings.ReplaceAll(k, " ", "_")
	return k
}

func isFullySensitiveKey(key string) bool {
	k := normalizeSettingKey(key)
	if k == "" {
		return false
	}
	if isDatabaseDSNKey(k) {
		return true
	}
	for _, sub := range sensitiveKeySubstrings {
		if strings.Contains(k, sub) {
			return true
		}
	}
	return false
}

// isDatabaseDSNKey matches connection strings whose names do not contain the
// generic secret substrings. These are the real DSN fields loaded from
// settings (db_uri, db_keys_uri, chatwoot_import_db_uri) plus the same shape
// of key nested under another prefix.
func isDatabaseDSNKey(k string) bool {
	switch k {
	case "db_uri", "db_keys_uri", "chatwoot_import_db_uri", "mcp_oauth_db_uri":
		return true
	}
	if strings.HasSuffix(k, "_db_uri") || strings.HasSuffix(k, "db_keys_uri") || strings.HasSuffix(k, "_dsn") {
		return true
	}
	return false
}

func isProxyKey(key string) bool {
	return strings.Contains(normalizeSettingKey(key), "proxy")
}

// proxyValueMayContainUserinfo reports whether a proxy value embeds
// credentials or is too malformed to prove that it does not.
func proxyValueMayContainUserinfo(value string) bool {
	s := strings.TrimSpace(value)
	if s == "" {
		return false
	}
	// Without a host the parse proves nothing: "user:pass@host:1080" reads as
	// scheme "user" with the credentials left in Opaque.
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return u.User != nil
	}
	return strings.Contains(s, "@")
}

func redactURLSecretsInText(s string) string {
	if !strings.Contains(s, "://") {
		return s
	}
	s = maskURLUserinfo(s)
	return maskSecretQueryValues(s)
}

// maskSecretQueryValues masks the value of each query parameter whose name
// looks secret as written or percent-decoded, keeping the rest of the text.
// The value runs to the next '&', '#' or whitespace because commas are legal
// in it, so a later URL in the same comma-separated list is masked with it.
func maskSecretQueryValues(s string) string {
	var b strings.Builder
	prev := 0
	for _, m := range queryParamName.FindAllStringSubmatchIndex(s, -1) {
		if m[0] < prev {
			continue // inside a value that is already masked
		}
		name := s[m[2]:m[3]]
		decoded, _ := url.QueryUnescape(name)
		if !secretQueryName.MatchString(name) && !secretQueryName.MatchString(decoded) {
			continue
		}
		b.WriteString(s[prev:m[1]])
		b.WriteString(redactedSecret)
		prev = len(s)
		if end := strings.IndexAny(s[m[1]:], "&#\t\n\f\r "); end >= 0 {
			prev = m[1] + end
		}
	}
	b.WriteString(s[prev:])
	return b.String()
}

// maskURLUserinfo masks everything between "://" and the last '@' before the
// next URL or whitespace. Userinfo may hold '/', '?', '#', ',' or quotes, so
// the authority is not parsed; an '@' later in the path is masked too, which
// only hides more. A later scheme starts the next URL right after a comma or
// once whitespace has ended the current one; otherwise, as in a password
// holding "https://", it stays part of the current URL.
func maskURLUserinfo(s string) string {
	var starts [][]int
	for _, loc := range urlScheme.FindAllStringIndex(s, -1) {
		if n := len(starts); n > 0 && s[loc[0]-1] != ',' &&
			strings.IndexFunc(s[starts[n-1][1]:loc[0]], unicode.IsSpace) < 0 {
			continue
		}
		starts = append(starts, loc)
	}
	var b strings.Builder
	prev := 0
	for i, loc := range starts {
		end := len(s)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		segment := s[loc[1]:end]
		if space := strings.IndexFunc(segment, unicode.IsSpace); space >= 0 {
			segment = segment[:space]
		}
		if at := strings.LastIndexByte(segment, '@'); at >= 0 {
			b.WriteString(s[prev:loc[1]])
			b.WriteString(redactedSecret)
			prev = loc[1] + at
		}
	}
	b.WriteString(s[prev:])
	return b.String()
}

// FormatProxyParseFailure describes a proxy URL for logs when url.Parse (or
// SetProxyAddress) fails. url.Error.Error includes the original URL, so
// callers must log this string instead of the parser error.
func FormatProxyParseFailure(raw string) string {
	scheme, host := safeProxySchemeHost(raw)
	msg := "parse failure"
	switch {
	case scheme != "" && host != "":
		msg = fmt.Sprintf("scheme=%s host=%s: parse failure", scheme, host)
	case scheme != "":
		msg = fmt.Sprintf("scheme=%s: parse failure", scheme)
	case host != "":
		msg = fmt.Sprintf("host=%s: parse failure", host)
	}
	if info := proxyUserinfo(raw); strings.Contains(info, ":") && strings.Contains(msg, info) {
		return "parse failure"
	}
	return msg
}

func safeProxySchemeHost(raw string) (scheme, host string) {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"'`)
	if u, err := url.Parse(s); err == nil && validProxyScheme(u.Scheme) && safeProxyHost(u.Host) {
		return u.Scheme, u.Host
	}
	return manualProxySchemeHost(s)
}

func manualProxySchemeHost(s string) (scheme, host string) {
	rest := s
	if i := strings.Index(s, "://"); i >= 0 {
		candidate := s[:i]
		if !validProxyScheme(candidate) {
			return "", ""
		}
		scheme = candidate
		rest = s[i+3:]
	}
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	} else if strings.Contains(s, "://") {
		// No userinfo separator. A failed parse of "scheme://user:pass" would
		// otherwise treat the credentials as a host:port pair.
		if !safeProxyHost(cutHostToken(rest)) {
			return scheme, ""
		}
	}
	host = cutHostToken(rest)
	if !safeProxyHost(host) {
		host = ""
	}
	return scheme, host
}

func cutHostToken(rest string) string {
	if i := strings.IndexAny(rest, "/?# \t\r\n\"'"); i >= 0 {
		rest = rest[:i]
	}
	// Drop a port that is not all digits: it may be a secret. A ':' before the
	// last ']' belongs to an IPv6 literal.
	if i := strings.LastIndexByte(rest, ':'); i > strings.LastIndexByte(rest, ']') && !isAllDigits(rest[i+1:]) {
		rest = rest[:i]
	}
	return rest
}

func proxyUserinfo(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, `"'`)
	rest := s
	if i := strings.Index(s, "://"); i >= 0 {
		rest = s[i+3:]
	}
	at := strings.LastIndex(rest, "@")
	if at <= 0 {
		return ""
	}
	return rest[:at]
}

func validProxyScheme(scheme string) bool {
	if scheme == "" || len(scheme) > 32 {
		return false
	}
	for i := 0; i < len(scheme); i++ {
		c := scheme[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return true
}

func safeProxyHost(host string) bool {
	if host == "" || len(host) > 300 || strings.ContainsAny(host, "@/?# \t\r\n\\") {
		return false
	}
	for i := 0; i < len(host); i++ {
		if host[i] < 0x21 || host[i] > 0x7e {
			return false
		}
	}
	if strings.HasPrefix(host, "[") {
		end := strings.Index(host, "]")
		if end < 2 {
			return false
		}
		if end == len(host)-1 {
			return true
		}
		return host[end+1] == ':' && isAllDigits(host[end+2:])
	}
	if strings.Count(host, ":") > 1 {
		return false
	}
	name, port, hasPort := strings.Cut(host, ":")
	if name == "" || (hasPort && !isAllDigits(port)) {
		return false
	}
	plausibleName := name == "localhost" || strings.Contains(name, ".") || isAllDigits(name)
	if !plausibleName && !hasPort {
		return false
	}
	return true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
