# Embedded MCP OAuth Design

## Goal

Add an opt-in OAuth 2.1 authorization server for the existing streamable-HTTP MCP endpoint so remote clients such as Claude custom connectors can authenticate without custom Basic Auth headers, while preserving existing Basic Auth behavior.

## Scope

- OAuth is disabled by default with `MCP_OAUTH_ENABLED=false`.
- `MCP_OAUTH_ISSUER_URL` is required when OAuth is enabled and must be a public HTTPS issuer URL.
- `MCP_OAUTH_RESOURCE_URL` is optional. When omitted, it is derived from the issuer origin plus `APP_BASE_PATH` and `/mcp`.
- The OAuth authorization UI authenticates against the existing `APP_BASIC_AUTH` account pairs. OAuth startup is rejected if no Basic Auth accounts exist.
- `/mcp` accepts either a valid OAuth Bearer token or an existing valid Basic credential when OAuth is enabled.
- OAuth state is persisted in `storages/oauth.db` through the repository's existing CGO/pure-Go SQLite driver abstraction.
- No external identity provider, JWT signing key, or new Go dependency is introduced.

## Protocol surface

The embedded authorization server exposes:

- RFC 9728 protected-resource metadata at the well-known URL derived from the canonical MCP resource URL.
- RFC 8414 authorization-server metadata at the well-known URL derived from the issuer URL.
- `GET/POST <issuer-path>/oauth/authorize` for authorization-code login.
- `POST <issuer-path>/oauth/token` for authorization-code and refresh-token grants.
- `POST <issuer-path>/oauth/register` for RFC 7591 Dynamic Client Registration compatibility with Claude.

DCR is implemented only as a compatibility path and the metadata does not claim Client ID Metadata Document support. The storage/client lookup boundary must remain separable so CIMD can be added later without rewriting token issuance.

## Security invariants

- Authorization code flow requires PKCE with `code_challenge_method=S256`.
- `resource` is required in authorization-code and token requests and must exactly match the configured canonical MCP resource (RFC 8707 audience binding).
- Authorization responses include `iss` matching the configured issuer (RFC 9207).
- Redirect URIs are exact-match only. HTTPS is required except native loopback redirects.
- Authorization codes, access tokens, and refresh tokens are cryptographically random opaque values. Only SHA-256 hashes are stored.
- Authorization codes are single-use and short-lived.
- Access tokens are short-lived.
- Refresh tokens rotate on every use. Reuse of a revoked refresh token revokes the entire token family, including access tokens.
- OAuth HTML and token responses use `no-store`; the login page is frame-denied and has a restrictive CSP.
- OAuth endpoints and the OAuth-protected `/mcp` route are registered before the application's global Basic Auth middleware. Other REST/UI routes keep their current Basic Auth behavior.

## Persistence

`storages/oauth.db` owns three logical tables:

1. Dynamic clients: client id, exact redirect URI list, display name, application type, creation time.
2. Authorization codes: code hash, client, subject, redirect URI, PKCE challenge, resource, scope, expiry, used time.
3. Tokens: token hash, type, refresh family, client, subject, resource, scope, expiry, revocation time.

SQLite transactions make authorization-code redemption and refresh rotation atomic.

## Compatibility

With OAuth disabled, routing and authentication remain unchanged. With OAuth enabled, REST/UI routes still use Basic Auth while `/mcp` accepts Basic or Bearer. `APP_BASE_PATH` remains the MCP route prefix; RFC well-known endpoints use the standards-required path-insertion form and therefore reverse proxies must forward those root `.well-known` paths.

## Tests

Tests cover metadata, DCR redirect validation, PKCE, authorization-code single use, resource mismatch, Bearer challenge/validation, Basic fallback, refresh rotation/reuse family revocation, issuer response parameter, and base-path well-known URL derivation.
