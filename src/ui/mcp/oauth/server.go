package oauth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

const (
	defaultStorageURI    = "file:storages/oauth.db"
	defaultScope         = "mcp"
	codeTTL              = 5 * time.Minute
	accessTokenTTL       = time.Hour
	refreshTokenTTL      = 30 * 24 * time.Hour
	maxOAuthBodyBytes    = 16 * 1024
	maxClientNameBytes   = 200
	maxRedirectURIBytes  = 2048
	maxRedirectURIsBytes = 8 * 1024
)

type Server struct {
	issuer             *url.URL
	resource           *url.URL
	store              *store
	validateCredential CredentialValidator
	now                func() time.Time
}

type dynamicRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	ApplicationType         string   `json:"application_type"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

type authorizationRequest struct {
	ResponseType        string `form:"response_type"`
	ClientID            string `form:"client_id"`
	RedirectURI         string `form:"redirect_uri"`
	CodeChallenge       string `form:"code_challenge"`
	CodeChallengeMethod string `form:"code_challenge_method"`
	Resource            string `form:"resource"`
	Scope               string `form:"scope"`
	State               string `form:"state"`
	Username            string `form:"username"`
	Password            string `form:"password"`
}

type tokenRequest struct {
	GrantType    string `form:"grant_type"`
	ClientID     string `form:"client_id"`
	Code         string `form:"code"`
	RedirectURI  string `form:"redirect_uri"`
	CodeVerifier string `form:"code_verifier"`
	RefreshToken string `form:"refresh_token"`
	Resource     string `form:"resource"`
}

type authorizePageData struct {
	ClientName          string
	RedirectTarget      string
	LocalRedirect       bool
	AuthorizePath       string
	ResponseType        string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Resource            string
	Scope               string
	State               string
	Error               string
}

var authorizeTemplate = template.Must(template.New("oauth-authorize").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Authorize {{.ClientName}}</title>
  <style>body{font-family:system-ui,sans-serif;max-width:28rem;margin:4rem auto;padding:0 1rem}label{display:block;margin-top:1rem}input{box-sizing:border-box;width:100%;padding:.65rem}button{margin-top:1.25rem;padding:.7rem 1rem}.error{color:#a00}</style>
</head>
<body>
  <h1>Authorize {{.ClientName}}</h1>
  <p>Sign in with an existing GOWA Basic Auth account to allow this MCP client to access WhatsApp through GOWA.</p>
  {{if .LocalRedirect}}<p>After authorization, you'll return to <strong>{{.ClientName}}</strong> on this device (<strong>{{.RedirectTarget}}</strong>).</p>
  {{else}}<p>After authorization, you'll continue to <strong>{{.RedirectTarget}}</strong>.</p>{{end}}
  {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
  <form method="post" action="{{.AuthorizePath}}">
    <input type="hidden" name="response_type" value="{{.ResponseType}}">
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
    <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
    <input type="hidden" name="resource" value="{{.Resource}}">
    <input type="hidden" name="scope" value="{{.Scope}}">
    <input type="hidden" name="state" value="{{.State}}">
    <label>Username<input name="username" autocomplete="username" required></label>
    <label>Password<input type="password" name="password" autocomplete="current-password" required></label>
    <button type="submit">Authorize</button>
  </form>
</body>
</html>`))

func New(cfg Config, validateCredential CredentialValidator) (*Server, error) {
	if validateCredential == nil {
		return nil, errors.New("MCP OAuth requires a credential validator")
	}
	issuer, err := normalizePublicURL(cfg.IssuerURL, "issuer")
	if err != nil {
		return nil, err
	}
	resource, err := normalizePublicURL(cfg.ResourceURL, "resource")
	if err != nil {
		return nil, err
	}
	if issuer.Scheme != resource.Scheme || !strings.EqualFold(issuer.Host, resource.Host) {
		return nil, errors.New("MCP OAuth issuer and resource must use the same public origin")
	}
	storageURI := strings.TrimSpace(cfg.StorageURI)
	if storageURI == "" {
		storageURI = defaultStorageURI
	}
	store, err := openStore(storageURI)
	if err != nil {
		return nil, err
	}
	return &Server{
		issuer:             issuer,
		resource:           resource,
		store:              store,
		validateCredential: validateCredential,
		now:                func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	return s.store.Close()
}

func DefaultResourceURL(issuerURL, appBasePath string) (string, error) {
	issuer, err := normalizePublicURL(issuerURL, "issuer")
	if err != nil {
		return "", err
	}
	resource := &url.URL{Scheme: issuer.Scheme, Host: issuer.Host}
	resource.Path = path.Join("/", strings.TrimSpace(appBasePath), "mcp")
	return resource.String(), nil
}

func (s *Server) RegisterPublic(app *fiber.App) {
	s.limitPublicPOSTBodies(app)
	app.Get(wellKnownPath("oauth-authorization-server", s.issuer.Path), s.authorizationServerMetadata)
	app.Get(wellKnownPath("oauth-protected-resource", s.resource.Path), s.protectedResourceMetadata)
	app.Post(s.issuerEndpointPath("/oauth/register"), s.registerClient)
	app.Get(s.issuerEndpointPath("/oauth/authorize"), s.authorizeGET)
	app.Post(s.issuerEndpointPath("/oauth/authorize"), s.authorizePOST)
	app.Post(s.issuerEndpointPath("/oauth/token"), s.token)
}

// limitPublicPOSTBodies applies the OAuth cap in fasthttp immediately after
// headers are received, before the unauthenticated request body is buffered.
func (s *Server) limitPublicPOSTBodies(app *fiber.App) {
	paths := []string{
		routingPathKey(s.issuerEndpointPath("/oauth/register")),
		routingPathKey(s.issuerEndpointPath("/oauth/authorize")),
		routingPathKey(s.issuerEndpointPath("/oauth/token")),
	}
	previous := app.Server().HeaderReceived
	app.Server().HeaderReceived = func(header *fasthttp.RequestHeader) fasthttp.RequestConfig {
		var cfg fasthttp.RequestConfig
		if previous != nil {
			cfg = previous(header)
		}
		if !bytes.Equal(header.Method(), []byte(fiber.MethodPost)) {
			return cfg
		}
		if cfg.MaxRequestBodySize != 0 && cfg.MaxRequestBodySize <= maxOAuthBodyBytes {
			return cfg
		}
		requestPath := routingPathKey(requestRoutingPath(header))
		if requestPath != "" && slices.Contains(paths, requestPath) {
			cfg.MaxRequestBodySize = maxOAuthBodyBytes
		}
		return cfg
	}
}

// requestRoutingPath extracts the path Fiber will route on using the same
// fasthttp URI parser Fiber itself uses, so query strings, fragments, and
// absolute-form request targets are stripped identically. An unparsable target
// yields an empty path in Fiber too, which cannot match an OAuth route.
func requestRoutingPath(header *fasthttp.RequestHeader) string {
	var uri fasthttp.URI
	if err := uri.Parse(header.Host(), header.RequestURI()); err != nil {
		return ""
	}
	return string(uri.PathOriginal())
}

// routingPathKey normalizes a path the way Fiber's router does under this
// application's defaults (CaseSensitive and StrictRouting both disabled), so the
// header-time cap covers every URL form that reaches the OAuth handlers rather
// than only the byte-exact registered path. Fiber lowercases ASCII only; for an
// ASCII-only registered path this can only widen the match, never narrow it.
func routingPathKey(p string) string {
	key := strings.ToLower(p)
	if len(key) > 1 && key[len(key)-1] == '/' {
		key = strings.TrimRight(key, "/")
	}
	return key
}

func (s *Server) MCPAuthMiddleware(basic CredentialValidator) fiber.Handler {
	return func(c fiber.Ctx) error {
		scheme, credentials := splitAuthorization(c.Get(fiber.HeaderAuthorization))
		switch strings.ToLower(scheme) {
		case "bearer":
			if credentials == "" {
				return s.mcpUnauthorized(c, basic != nil, true)
			}
			principal, err := s.store.validateAccessToken(c.Context(), credentials, s.resource.String(), s.now())
			if err != nil {
				return s.mcpUnauthorized(c, basic != nil, true)
			}
			c.Locals("oauth_subject", principal.Subject)
			c.Locals("oauth_client_id", principal.ClientID)
			return c.Next()
		case "basic":
			if basic == nil {
				return s.mcpUnauthorized(c, false, false)
			}
			username, password, ok := decodeBasicCredentials(credentials)
			if !ok || !basic(username, password) {
				return s.mcpUnauthorized(c, true, false)
			}
			return c.Next()
		default:
			return s.mcpUnauthorized(c, basic != nil, false)
		}
	}
}

func (s *Server) authorizationServerMetadata(c fiber.Ctx) error {
	s.setNoStore(c)
	return c.JSON(fiber.Map{
		"issuer":                                         s.issuer.String(),
		"authorization_endpoint":                         s.issuerEndpointURL("/oauth/authorize"),
		"token_endpoint":                                 s.issuerEndpointURL("/oauth/token"),
		"registration_endpoint":                          s.issuerEndpointURL("/oauth/register"),
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"code_challenge_methods_supported":               []string{"S256"},
		"scopes_supported":                               []string{defaultScope},
		"authorization_response_iss_parameter_supported": true,
	})
}

func (s *Server) protectedResourceMetadata(c fiber.Ctx) error {
	s.setNoStore(c)
	return c.JSON(fiber.Map{
		"resource":                 s.resource.String(),
		"authorization_servers":    []string{s.issuer.String()},
		"bearer_methods_supported": []string{"header"},
		"scopes_supported":         []string{defaultScope},
		"resource_name":            "GOWA MCP",
	})
}

func (s *Server) registerClient(c fiber.Ctx) error {
	s.setNoStore(c)
	if len(c.Body()) > maxOAuthBodyBytes {
		return oauthError(c, fiber.StatusRequestEntityTooLarge, "invalid_client_metadata", "registration document is too large")
	}
	var req dynamicRegistrationRequest
	if err := c.Bind().Body(&req); err != nil {
		return oauthError(c, fiber.StatusBadRequest, "invalid_client_metadata", "invalid registration document")
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 {
		return oauthError(c, fiber.StatusBadRequest, "invalid_redirect_uri", "redirect_uris must contain between 1 and 10 entries")
	}
	if req.ApplicationType == "" {
		req.ApplicationType = "web"
	}
	if req.ApplicationType != "web" && req.ApplicationType != "native" {
		return oauthError(c, fiber.StatusBadRequest, "invalid_client_metadata", "unsupported application_type")
	}
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "none"
	}
	if req.TokenEndpointAuthMethod != "none" {
		return oauthError(c, fiber.StatusBadRequest, "invalid_client_metadata", "only public clients are supported")
	}
	if !onlySupported(req.ResponseTypes, "code") || !onlySupported(req.GrantTypes, "authorization_code", "refresh_token") {
		return oauthError(c, fiber.StatusBadRequest, "invalid_client_metadata", "unsupported response_type or grant_type")
	}
	redirectBytes := 0
	for _, redirectURI := range req.RedirectURIs {
		if len(redirectURI) > maxRedirectURIBytes {
			return oauthError(c, fiber.StatusBadRequest, "invalid_redirect_uri", "redirect URI is too long")
		}
		redirectBytes += len(redirectURI)
		if !validRedirectURI(redirectURI, req.ApplicationType) {
			return oauthError(c, fiber.StatusBadRequest, "invalid_redirect_uri", "redirect URI must use HTTPS, except native loopback redirects")
		}
	}
	if redirectBytes > maxRedirectURIsBytes {
		return oauthError(c, fiber.StatusBadRequest, "invalid_redirect_uri", "redirect URIs are too large")
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > maxClientNameBytes {
		return oauthError(c, fiber.StatusBadRequest, "invalid_client_metadata", "client_name is too long")
	}
	clientID, err := randomSecret("gowa_client_", 24)
	if err != nil {
		return oauthError(c, fiber.StatusInternalServerError, "server_error", "could not register client")
	}
	client := Client{
		ID:                      clientID,
		Name:                    name,
		RedirectURIs:            append([]string(nil), req.RedirectURIs...),
		ApplicationType:         req.ApplicationType,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		CreatedAt:               s.now(),
	}
	if err := s.store.registerClient(c.Context(), client); err != nil {
		if errors.Is(err, ErrRegistrationLimit) {
			return oauthError(c, fiber.StatusTooManyRequests, "temporarily_unavailable", "dynamic client registration limit reached; retry later")
		}
		return oauthError(c, fiber.StatusInternalServerError, "server_error", "could not register client")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"client_id":                  client.ID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"application_type":           client.ApplicationType,
		"token_endpoint_auth_method": client.TokenEndpointAuthMethod,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

func (s *Server) authorizeGET(c fiber.Ctx) error {
	req := authorizationRequest{
		ResponseType:        c.Query("response_type"),
		ClientID:            c.Query("client_id"),
		RedirectURI:         c.Query("redirect_uri"),
		CodeChallenge:       c.Query("code_challenge"),
		CodeChallengeMethod: c.Query("code_challenge_method"),
		Resource:            c.Query("resource"),
		Scope:               c.Query("scope"),
		State:               c.Query("state"),
	}
	client, valid, err := s.validateAuthorizationRequest(c, &req)
	if err != nil {
		return err
	}
	if !valid {
		return nil
	}
	return s.renderAuthorize(c, fiber.StatusOK, req, client, "")
}

func (s *Server) authorizePOST(c fiber.Ctx) error {
	var req authorizationRequest
	if err := c.Bind().Body(&req); err != nil {
		return oauthError(c, fiber.StatusBadRequest, "invalid_request", "invalid authorization request")
	}
	client, valid, err := s.validateAuthorizationRequest(c, &req)
	if err != nil {
		return err
	}
	if !valid {
		return nil
	}
	if !s.validateCredential(req.Username, req.Password) {
		return s.renderAuthorize(c, fiber.StatusUnauthorized, req, client, "Invalid username or password.")
	}
	code, err := s.store.issueAuthorizationCode(c.Context(), AuthorizationGrant{
		ClientID:      req.ClientID,
		Subject:       req.Username,
		RedirectURI:   req.RedirectURI,
		CodeChallenge: req.CodeChallenge,
		Resource:      req.Resource,
		Scope:         req.Scope,
	}, s.now(), codeTTL)
	if err != nil {
		return oauthError(c, fiber.StatusInternalServerError, "server_error", "could not issue authorization code")
	}
	redirectURL, err := url.Parse(req.RedirectURI)
	if err != nil {
		return oauthError(c, fiber.StatusBadRequest, "invalid_request", "redirect_uri is not a valid URI")
	}
	query := redirectURL.Query()
	query.Set("code", code)
	if req.State != "" {
		query.Set("state", req.State)
	}
	query.Set("iss", s.issuer.String())
	redirectURL.RawQuery = query.Encode()
	s.setNoStore(c)
	return c.Redirect().Status(fiber.StatusFound).To(redirectURL.String())
}

func (s *Server) token(c fiber.Ctx) error {
	s.setNoStore(c)
	var req tokenRequest
	if err := c.Bind().Body(&req); err != nil {
		return oauthError(c, fiber.StatusBadRequest, "invalid_request", "invalid token request")
	}
	if req.Resource != s.resource.String() {
		return oauthError(c, fiber.StatusBadRequest, "invalid_target", "resource does not match this MCP server")
	}
	var (
		pair TokenPair
		err  error
	)
	switch req.GrantType {
	case "authorization_code":
		if !validPKCEVerifier(req.CodeVerifier) {
			return oauthError(c, fiber.StatusBadRequest, "invalid_grant", "invalid authorization code or PKCE verifier")
		}
		pair, err = s.store.exchangeAuthorizationCode(c.Context(), CodeExchange{
			ClientID:     req.ClientID,
			Code:         req.Code,
			RedirectURI:  req.RedirectURI,
			CodeVerifier: req.CodeVerifier,
			Resource:     req.Resource,
		}, s.now(), accessTokenTTL, refreshTokenTTL)
	case "refresh_token":
		pair, err = s.store.rotateRefreshToken(c.Context(), RefreshExchange{
			ClientID:     req.ClientID,
			RefreshToken: req.RefreshToken,
			Resource:     req.Resource,
		}, s.now(), accessTokenTTL, refreshTokenTTL)
	default:
		return oauthError(c, fiber.StatusBadRequest, "unsupported_grant_type", "supported grant types are authorization_code and refresh_token")
	}
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidTarget):
			return oauthError(c, fiber.StatusBadRequest, "invalid_target", "resource does not match the authorization grant")
		case errors.Is(err, ErrInvalidGrant), errors.Is(err, ErrRefreshReuse):
			return oauthError(c, fiber.StatusBadRequest, "invalid_grant", "authorization grant is invalid, expired, or already used")
		default:
			return oauthError(c, fiber.StatusInternalServerError, "server_error", "token request failed")
		}
	}
	return c.JSON(tokenResponse{
		AccessToken:  pair.AccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    pair.ExpiresIn,
		RefreshToken: pair.RefreshToken,
		Scope:        pair.Scope,
	})
}

func (s *Server) validateAuthorizationRequest(c fiber.Ctx, req *authorizationRequest) (Client, bool, error) {
	client, err := s.store.getClient(c.Context(), req.ClientID)
	if err != nil {
		return Client{}, false, oauthError(c, fiber.StatusBadRequest, "invalid_request", "unknown client_id")
	}
	if !redirectURIMatches(client, req.RedirectURI) {
		return Client{}, false, oauthError(c, fiber.StatusBadRequest, "invalid_request", "redirect_uri does not match the registered client")
	}
	// Once the client and redirect URI are known to be safe, OAuth errors must
	// be returned to the client at that redirect URI rather than as an endpoint
	// response in the user's browser.
	if req.ResponseType != "code" {
		return Client{}, false, s.authorizationError(c, *req, "unsupported_response_type", "response_type must be code")
	}
	if req.CodeChallengeMethod != "S256" || !validPKCEChallenge(req.CodeChallenge) {
		return Client{}, false, s.authorizationError(c, *req, "invalid_request", "PKCE S256 is required")
	}
	if req.Resource != s.resource.String() {
		return Client{}, false, s.authorizationError(c, *req, "invalid_target", "resource does not match this MCP server")
	}
	if req.Scope == "" {
		req.Scope = defaultScope
	}
	if req.Scope != defaultScope {
		return Client{}, false, s.authorizationError(c, *req, "invalid_scope", "only the mcp scope is supported")
	}
	return client, true, nil
}

func (s *Server) renderAuthorize(c fiber.Ctx, status int, req authorizationRequest, client Client, pageError string) error {
	s.setNoStore(c)
	c.Set(fiber.HeaderContentSecurityPolicy, authorizationPageCSP(req.RedirectURI))
	c.Set(fiber.HeaderXFrameOptions, "DENY")
	c.Set(fiber.HeaderReferrerPolicy, "no-referrer")
	c.Type("html", "utf-8")
	c.Status(status)
	redirectTarget, localRedirect := redirectDisplay(req.RedirectURI)
	return authorizeTemplate.Execute(c, authorizePageData{
		ClientName:          client.Name,
		RedirectTarget:      redirectTarget,
		LocalRedirect:       localRedirect,
		AuthorizePath:       s.issuerEndpointPath("/oauth/authorize"),
		ResponseType:        req.ResponseType,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Resource:            req.Resource,
		Scope:               req.Scope,
		State:               req.State,
		Error:               pageError,
	})
}

func authorizationPageCSP(redirectURI string) string {
	formAction := "'self'"
	if redirectOrigin := redirectOrigin(redirectURI); redirectOrigin != "" {
		// Chrome applies form-action across redirects. Allow only the origin of
		// the already-validated OAuth callback so the authorization POST can
		// complete its redirect without weakening the rest of the page policy.
		formAction += " " + redirectOrigin
	}
	return "default-src 'none'; style-src 'unsafe-inline'; form-action " + formAction + "; frame-ancestors 'none'; base-uri 'none'"
}

func redirectOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

func (s *Server) mcpUnauthorized(c fiber.Ctx, advertiseBasic, invalidBearer bool) error {
	challenge := fmt.Sprintf(`Bearer resource_metadata="%s", scope="%s"`, s.protectedResourceMetadataURL(), defaultScope)
	if invalidBearer {
		challenge += `, error="invalid_token"`
	}
	c.Set(fiber.HeaderWWWAuthenticate, challenge)
	if advertiseBasic {
		c.Append(fiber.HeaderWWWAuthenticate, `Basic realm="GOWA MCP"`)
	}
	s.setNoStore(c)
	return c.SendStatus(fiber.StatusUnauthorized)
}

func (s *Server) setNoStore(c fiber.Ctx) {
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set(fiber.HeaderPragma, "no-cache")
}

func (s *Server) issuerEndpointPath(suffix string) string {
	base := strings.TrimRight(s.issuer.Path, "/")
	if base == "" {
		return suffix
	}
	return base + suffix
}

func (s *Server) issuerEndpointURL(suffix string) string {
	u := *s.issuer
	u.Path = s.issuerEndpointPath(suffix)
	u.RawPath = ""
	return u.String()
}

func (s *Server) protectedResourceMetadataURL() string {
	u := *s.resource
	u.Path = wellKnownPath("oauth-protected-resource", s.resource.Path)
	u.RawPath = ""
	return u.String()
}

func normalizePublicURL(raw, label string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("MCP OAuth %s URL is required", label)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("MCP OAuth %s URL must be an absolute public HTTPS URL without credentials, query, or fragment", label)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

func wellKnownPath(metadataName, resourcePath string) string {
	cleaned := strings.Trim(resourcePath, "/")
	if cleaned == "" {
		return "/.well-known/" + metadataName
	}
	return "/.well-known/" + metadataName + "/" + cleaned
}

func validRedirectURI(raw, applicationType string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if applicationType != "native" || u.Scheme != "http" {
		return false
	}
	hostname := u.Hostname()
	// Claude Code registers its native callback as localhost with an explicit
	// ephemeral port. Accept that narrow compatibility form, but keep the
	// authorization-time port exception limited to IP literals in
	// loopbackRedirectMatches so localhost redirects remain exact matches.
	if strings.EqualFold(hostname, "localhost") {
		return u.Port() != ""
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

func redirectDisplay(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "the registered client", false
	}
	hostname := u.Hostname()
	if strings.EqualFold(hostname, "localhost") {
		return u.Host, true
	}
	ip := net.ParseIP(hostname)
	return u.Host, ip != nil && ip.IsLoopback()
}

func validPKCEChallenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil && len(decoded) == sha256.Size
}

func validPKCEVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for _, r := range verifier {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-._~", r) {
			continue
		}
		return false
	}
	return true
}

func containsExact(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// redirectURIMatches uses exact string matching for every registered redirect
// except the port of native loopback-IP redirects. OAuth 2.1 requires that
// exception so desktop and CLI clients can bind an ephemeral local port.
func redirectURIMatches(client Client, requested string) bool {
	for _, registered := range client.RedirectURIs {
		if registered == requested {
			return true
		}
		if client.ApplicationType == "native" && loopbackRedirectMatches(registered, requested) {
			return true
		}
	}
	return false
}

func loopbackRedirectMatches(registered, requested string) bool {
	registeredURL, err := url.Parse(registered)
	if err != nil {
		return false
	}
	requestedURL, err := url.Parse(requested)
	if err != nil {
		return false
	}
	if registeredURL.Scheme != "http" || requestedURL.Scheme != "http" ||
		registeredURL.User != nil || requestedURL.User != nil ||
		registeredURL.Fragment != "" || requestedURL.Fragment != "" {
		return false
	}
	registeredIP := net.ParseIP(registeredURL.Hostname())
	requestedIP := net.ParseIP(requestedURL.Hostname())
	if registeredIP == nil || requestedIP == nil || !registeredIP.IsLoopback() || !requestedIP.IsLoopback() ||
		registeredURL.Hostname() != requestedURL.Hostname() {
		return false
	}
	return registeredURL.EscapedPath() == requestedURL.EscapedPath() &&
		registeredURL.RawQuery == requestedURL.RawQuery &&
		registeredURL.ForceQuery == requestedURL.ForceQuery
}

func onlySupported(values []string, supported ...string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if !containsExact(supported, value) {
			return false
		}
	}
	return true
}

func splitAuthorization(header string) (string, string) {
	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func decodeBasicCredentials(encoded string) (string, string, bool) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", false
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	if !ok || username == "" {
		return "", "", false
	}
	return username, password, true
}

func oauthError(c fiber.Ctx, status int, code, description string) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set(fiber.HeaderPragma, "no-cache")
	return c.Status(status).JSON(fiber.Map{
		"error":             code,
		"error_description": description,
	})
}

func (s *Server) authorizationError(c fiber.Ctx, req authorizationRequest, code, description string) error {
	redirectURL, err := url.Parse(req.RedirectURI)
	if err != nil {
		return oauthError(c, fiber.StatusBadRequest, code, description)
	}
	query := redirectURL.Query()
	query.Set("error", code)
	query.Set("error_description", description)
	if req.State != "" {
		query.Set("state", req.State)
	}
	query.Set("iss", s.issuer.String())
	redirectURL.RawQuery = query.Encode()
	s.setNoStore(c)
	return c.Redirect().Status(fiber.StatusFound).To(redirectURL.String())
}
