package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Client ID Metadata Documents (draft-ietf-oauth-client-id-metadata-document).
// A client_id that is an HTTPS URL names a JSON document, served at that URL,
// that describes the client. There is no registration and no client table to
// consult: the document is fetched (from an allowlisted host only), checked,
// and cached briefly. Claude identifies itself this way.

// CIMDConfig turns on CIMD client ids.
type CIMDConfig struct {
	// AllowedHosts are the exact hosts whose documents are fetched
	// (case-insensitive). A client_id on any other host is refused without
	// a request being made.
	AllowedHosts []string
	// HTTPClient makes the fetch. Required: the caller supplies one whose
	// dialer refuses non-public addresses (the client_id is attacker-chosen
	// input to a server-side fetch). Redirects are never followed whatever
	// the client's own policy.
	HTTPClient *http.Client
	// MaxBytes caps the document; default 16 KiB. The body is abandoned
	// mid-stream past the cap.
	MaxBytes int64
	// CacheTTL keeps a good document this long; default 5 minutes. Failures
	// are not cached.
	CacheTTL time.Duration
	// UserAgent is sent on every fetch; default "WTF-connector-check/1".
	UserAgent string
}

const (
	defaultCIMDMaxBytes  = 16 * 1024
	defaultCIMDCacheTTL  = 5 * time.Minute
	defaultCIMDUserAgent = "WTF-connector-check/1"
	maxCIMDCacheEntries  = 32
	maxCIMDRedirectURIs  = 10
)

// errCIMD is the one error a refused document surfaces as: the details stay
// in the wrapped error for logs, never in the browser.
var errCIMD = errors.New("client metadata document refused")

type cimdDocument struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

type cimdEntry struct {
	client  Client
	expires time.Time
}

type cimdResolver struct {
	allowed   map[string]bool
	client    *http.Client
	maxBytes  int64
	ttl       time.Duration
	userAgent string
	now       func() time.Time

	mu    sync.Mutex
	cache map[string]cimdEntry
}

func newCIMDResolver(cfg *CIMDConfig, now func() time.Time) (*cimdResolver, error) {
	if cfg.HTTPClient == nil {
		return nil, errors.New("MCP OAuth CIMD requires an HTTP client that refuses non-public addresses")
	}
	if len(cfg.AllowedHosts) == 0 {
		return nil, errors.New("MCP OAuth CIMD requires at least one allowed client host")
	}
	r := &cimdResolver{
		allowed:   map[string]bool{},
		maxBytes:  cfg.MaxBytes,
		ttl:       cfg.CacheTTL,
		userAgent: strings.TrimSpace(cfg.UserAgent),
		now:       now,
		cache:     map[string]cimdEntry{},
	}
	for _, h := range cfg.AllowedHosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			r.allowed[h] = true
		}
	}
	if r.maxBytes <= 0 {
		r.maxBytes = defaultCIMDMaxBytes
	}
	if r.ttl <= 0 {
		r.ttl = defaultCIMDCacheTTL
	}
	if r.userAgent == "" {
		r.userAgent = defaultCIMDUserAgent
	}
	// A copy, so the caller's client keeps its own redirect policy while
	// this one never follows any: Claude's documents redirect zero times,
	// and refusing is simpler than re-checking every hop.
	c := *cfg.HTTPClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	r.client = &c
	return r, nil
}

func isCIMDClientID(clientID string) bool { return strings.HasPrefix(clientID, "https://") }

// checkURL validates a CIMD client_id before anything is fetched.
func (r *cimdResolver) checkURL(clientID string) (*url.URL, error) {
	u, err := url.Parse(clientID)
	if err != nil {
		return nil, fmt.Errorf("%w: not a URL", errCIMD)
	}
	if u.Scheme != "https" || u.User != nil || u.Fragment != "" || strings.Contains(clientID, "#") || u.Opaque != "" {
		return nil, fmt.Errorf("%w: must be an https URL without credentials or fragment", errCIMD)
	}
	if u.Path == "" || u.Path == "/" {
		return nil, fmt.Errorf("%w: must have a path", errCIMD)
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return nil, fmt.Errorf("%w: dot segments", errCIMD)
		}
	}
	if p := u.Port(); p != "" && p != "443" {
		return nil, fmt.Errorf("%w: non-default port", errCIMD)
	}
	if !r.allowed[strings.ToLower(u.Hostname())] {
		return nil, fmt.Errorf("%w: host not allowed", errCIMD)
	}
	return u, nil
}

// hostAllowed is the cheap check the token endpoint repeats: the client's
// host is still on the list.
func (r *cimdResolver) hostAllowed(clientID string) bool {
	_, err := r.checkURL(clientID)
	return err == nil
}

// resolve returns the client a CIMD client_id describes.
func (r *cimdResolver) resolve(ctx context.Context, clientID string) (Client, error) {
	if _, err := r.checkURL(clientID); err != nil {
		return Client{}, err
	}
	now := r.now()
	r.mu.Lock()
	if e, ok := r.cache[clientID]; ok && now.Before(e.expires) {
		r.mu.Unlock()
		return e.client, nil
	}
	r.mu.Unlock()

	doc, err := r.fetch(ctx, clientID)
	if err != nil {
		return Client{}, err
	}
	client, err := validateCIMDDocument(clientID, doc)
	if err != nil {
		return Client{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cache) >= maxCIMDCacheEntries {
		for k, e := range r.cache {
			if !now.Before(e.expires) {
				delete(r.cache, k)
			}
		}
		for k := range r.cache {
			if len(r.cache) < maxCIMDCacheEntries {
				break
			}
			delete(r.cache, k)
		}
	}
	r.cache[clientID] = cimdEntry{client: client, expires: now.Add(r.ttl)}
	return client, nil
}

func (r *cimdResolver) fetch(ctx context.Context, clientID string) (cimdDocument, error) {
	// GET only: Claude's document endpoints answer 405 to HEAD.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return cimdDocument{}, fmt.Errorf("%w: %v", errCIMD, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", r.userAgent)
	resp, err := r.client.Do(req)
	if err != nil {
		return cimdDocument{}, fmt.Errorf("%w: fetch failed: %v", errCIMD, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cimdDocument{}, fmt.Errorf("%w: status %d", errCIMD, resp.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return cimdDocument{}, fmt.Errorf("%w: content type is not application/json", errCIMD)
	}
	// Read at most one byte past the cap, then stop: the rest of an
	// oversized body is never transferred into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, r.maxBytes+1))
	if err != nil {
		return cimdDocument{}, fmt.Errorf("%w: read failed: %v", errCIMD, err)
	}
	if int64(len(body)) > r.maxBytes {
		return cimdDocument{}, fmt.Errorf("%w: document too large", errCIMD)
	}
	var doc cimdDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return cimdDocument{}, fmt.Errorf("%w: not JSON", errCIMD)
	}
	return doc, nil
}

func validateCIMDDocument(clientID string, doc cimdDocument) (Client, error) {
	// Self-reference: without it, anything that echoes attacker JSON on an
	// allowed host could claim to be any client.
	if doc.ClientID != clientID {
		return Client{}, fmt.Errorf("%w: client_id does not name its own URL", errCIMD)
	}
	if len(doc.RedirectURIs) == 0 || len(doc.RedirectURIs) > maxCIMDRedirectURIs {
		return Client{}, fmt.Errorf("%w: redirect_uris must hold 1 to %d entries", errCIMD, maxCIMDRedirectURIs)
	}
	for _, r := range doc.RedirectURIs {
		if len(r) > maxRedirectURIBytes || !validCIMDRedirect(r) {
			return Client{}, fmt.Errorf("%w: invalid redirect URI", errCIMD)
		}
	}
	if doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != "none" {
		return Client{}, fmt.Errorf("%w: only public clients are supported", errCIMD)
	}
	// The document lists what the client can do, not what it is granted.
	// Claude's own document also names the jwt-bearer grant (measured
	// 2026-11-23), so extra types are ignored: this server only ever runs
	// the authorization-code flow, which the client must support.
	if len(doc.GrantTypes) > 0 && !slices.Contains(doc.GrantTypes, "authorization_code") {
		return Client{}, fmt.Errorf("%w: the client does not use the authorization code grant", errCIMD)
	}
	if len(doc.ResponseTypes) > 0 && !slices.Contains(doc.ResponseTypes, "code") {
		return Client{}, fmt.Errorf("%w: the client does not use response type code", errCIMD)
	}
	name := strings.TrimSpace(doc.ClientName)
	if len(name) > maxClientNameBytes {
		name = name[:maxClientNameBytes]
	}
	return Client{
		ID:                      clientID,
		Name:                    name,
		RedirectURIs:            slices.Clone(doc.RedirectURIs),
		ApplicationType:         applicationTypeCIMD,
		TokenEndpointAuthMethod: "none",
	}, nil
}

// validCIMDRedirect: https anywhere, or http on a loopback host (a native
// client's local callback). No credentials, no fragment.
func validCIMDRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		return isLoopbackHost(u.Hostname())
	}
	return false
}

// cimdRedirectMatches compares a requested redirect URI with the declared
// ones: exact on scheme, host, path and the parsed query (order does not
// matter, an extra or missing pair does). For a loopback host the port is
// ignored, and only the port: a native client such as Claude Code declares
// http://localhost/callback and arrives on an ephemeral port. localhost and
// 127.0.0.1 are different hosts and never match each other. For other hosts
// effective ports are compared, so https://x/cb equals https://x:443/cb.
func cimdRedirectMatches(declared []string, requested string) bool {
	req, err := url.Parse(requested)
	if err != nil || req.User != nil || req.Fragment != "" || strings.Contains(requested, "#") || req.Opaque != "" {
		return false
	}
	reqQuery, err := url.ParseQuery(req.RawQuery)
	if err != nil {
		return false
	}
	for _, d := range declared {
		dec, err := url.Parse(d)
		if err != nil {
			continue
		}
		if !strings.EqualFold(dec.Scheme, req.Scheme) || !strings.EqualFold(dec.Hostname(), req.Hostname()) {
			continue
		}
		if dec.EscapedPath() != req.EscapedPath() {
			continue
		}
		decQuery, err := url.ParseQuery(dec.RawQuery)
		if err != nil || !sameQuery(decQuery, reqQuery) {
			continue
		}
		if !isLoopbackHost(req.Hostname()) && effectivePort(dec) != effectivePort(req) {
			continue
		}
		return true
	}
	return false
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

func sameQuery(a, b url.Values) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok || len(av) != len(bv) {
			return false
		}
		as, bs := slices.Clone(av), slices.Clone(bv)
		slices.Sort(as)
		slices.Sort(bs)
		if !slices.Equal(as, bs) {
			return false
		}
	}
	return true
}
