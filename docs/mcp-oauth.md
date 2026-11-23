# MCP OAuth

GOWA can embed an OAuth 2.1 authorization server for its streamable-HTTP MCP endpoint. This is useful for remote MCP clients such as Claude custom connectors that cannot attach GOWA's existing Basic Auth header themselves.

OAuth is **disabled by default**. When it is enabled:

- `/mcp` accepts either `Authorization: Bearer <token>` or the existing Basic Auth credentials.
- The browser authorization page authenticates against the same `APP_BASIC_AUTH` accounts used by the REST API; there is no second user database.
- OAuth clients, authorization codes, and token hashes are persisted in `storages/oauth.db` by default.
- Existing REST/UI authentication is unchanged.

## Configuration

A minimal public deployment without `APP_BASE_PATH` looks like this:

```env
APP_BASIC_AUTH=admin:replace-with-a-strong-password
MCP_ENABLED=true
MCP_OAUTH_ENABLED=true
MCP_OAUTH_ISSUER_URL=https://gowa.example.com
# Optional; defaults to issuer origin + APP_BASE_PATH + /mcp
MCP_OAUTH_RESOURCE_URL=https://gowa.example.com/mcp
```

`MCP_OAUTH_ISSUER_URL` and the canonical MCP resource must be public HTTPS URLs. OAuth-enabled startup fails if `APP_BASIC_AUTH` is empty or the public URL configuration is invalid.

The equivalent CLI flags are:

```text
--mcp-oauth-enabled
--mcp-oauth-issuer-url
--mcp-oauth-resource-url
--mcp-oauth-db-uri
```

## Claude custom connector

Expose the GOWA MCP resource over HTTPS, then use the canonical MCP URL when adding the custom connector:

```text
https://gowa.example.com/mcp
```

Claude can dynamically register itself with GOWA and start the authorization-code flow. The browser will show GOWA's authorization page; sign in with one of the users from `APP_BASIC_AUTH`. The page displays both the registered client name and the redirect hostname before credentials are submitted.

GOWA implements RFC 7591 Dynamic Client Registration for compatibility with clients such as Claude. DCR is retained by the current MCP specification for backwards compatibility; GOWA does not advertise Client ID Metadata Document support until that mechanism is implemented.

Because registration is unauthenticated, GOWA limits registration documents to 16 KiB, each redirect URI to 2 KiB, new registrations to 40 per hour, and stored dynamic clients to 1,000. Registrations that remain unused for 24 hours are pruned before accepting new clients. These durable global limits protect the OAuth SQLite database across restarts. If a deployment still reaches the client cap, remove obsolete rows from `oauth_clients` during a maintenance window or replace the OAuth database to start with a new authorization-server state; deleting clients also invalidates their grants and tokens.

## `APP_BASE_PATH`

For a deployment using:

```env
APP_BASE_PATH=/gowa
MCP_OAUTH_ISSUER_URL=https://gowa.example.com/gowa
```

the default canonical resource is:

```text
https://gowa.example.com/gowa/mcp
```

OAuth endpoints are under the issuer path:

```text
https://gowa.example.com/gowa/oauth/authorize
https://gowa.example.com/gowa/oauth/token
https://gowa.example.com/gowa/oauth/register
```

RFC 8414 and RFC 9728 use **path-insertion** well-known URLs, not `APP_BASE_PATH/.well-known/...` URLs. The reverse proxy must therefore route these root paths to GOWA:

```text
https://gowa.example.com/.well-known/oauth-authorization-server/gowa
https://gowa.example.com/.well-known/oauth-protected-resource/gowa/mcp
```

The MCP `401 Unauthorized` challenge includes the exact protected-resource metadata URL, so conforming clients do not have to guess it.

## Cloudflare / reverse proxies

The configured issuer and resource are the **public** URLs seen by the MCP client, not the container's internal address. For example, do not configure `http://gowa:3000` when the public endpoint is `https://gowa.example.com`.

If Cloudflare Access, an authentication proxy, or a bot challenge sits in front of GOWA, make sure it does not replace the OAuth/MCP responses with its own interactive login or challenge. Claude must be able to reach:

- the RFC well-known metadata paths;
- `<issuer>/oauth/register`;
- `<issuer>/oauth/authorize`;
- `<issuer>/oauth/token`;
- the canonical MCP resource itself.

TLS termination at the reverse proxy is fine; `MCP_OAUTH_ISSUER_URL` and `MCP_OAUTH_RESOURCE_URL` must still describe their externally reachable HTTPS forms.

The authorization form validates the same Basic Auth credentials as the rest of GOWA. Internet-facing deployments should apply per-IP failed-login rate limiting and logging at the reverse proxy for `<issuer>/oauth/authorize`, just as they should for the normal Basic Auth surface.

## Security model

The implementation intentionally uses opaque tokens rather than JWTs:

- authorization codes, access tokens, and refresh tokens are generated with `crypto/rand`;
- only SHA-256 hashes are persisted;
- authorization codes are single-use and expire after 5 minutes;
- access tokens expire after 1 hour;
- refresh tokens rotate on each use;
- reusing a rotated refresh token revokes the whole token family;
- PKCE with `S256` is mandatory;
- redirect URIs must exactly match the URI registered by the client, except that native loopback-IP clients may change only the port used for their local callback;
- redirect URIs must use HTTPS, except `native` clients, which may use `http` with a loopback IP literal such as `http://127.0.0.1:PORT/callback` or `http://[::1]:PORT/callback`. For Claude Code compatibility, the exact hostname `localhost` is also accepted only with an explicit port; unlike loopback IP literals, a registered `localhost` redirect receives no dynamic-port exception and must match exactly during authorization;
- the OAuth `resource` value is bound to the canonical MCP resource and checked when access tokens are used;
- successful authorization responses include RFC 9207 `iss` for authorization-server mix-up protection.

Expired authorization codes and tokens are removed opportunistically during OAuth writes so their rows do not accumulate indefinitely. SQLite can reuse the freed pages; shrinking the file itself still requires normal SQLite maintenance such as `VACUUM` while GOWA is stopped.

No OAuth token is accepted as authentication for the normal REST/UI routes. OAuth only extends the MCP authentication surface.

## Discovery checks

For a root deployment, an unauthenticated MCP request should return a Bearer challenge pointing at protected-resource metadata:

```bash
curl -i -X POST https://gowa.example.com/mcp
```

Metadata can then be inspected directly:

```bash
curl https://gowa.example.com/.well-known/oauth-protected-resource/mcp
curl https://gowa.example.com/.well-known/oauth-authorization-server
```

The authorization-server metadata advertises authorization-code and refresh-token grants, PKCE `S256`, and the DCR registration endpoint.

## Multi-user mode (WTF)

With `WTF_MULTIUSER=true` the same OAuth server runs in a different mode, and
`/mcp` serves each person their own WhatsApp numbers only.

- **On by default.** `WTF_MCP_ENABLED` (default `true`) turns it on; the
  `MCP_OAUTH_*` settings and `APP_BASIC_AUTH` are not used. The issuer is the
  public URL (`WTF_PUBLIC_URL`, else `RENDER_EXTERNAL_URL`) and the resource is
  `<public URL>/mcp`, from that one setting. The public URL must be `https`;
  with a plain-http one (local development) MCP stays off with a warning.
- **Who the subject is.** The browser's WTF sign-in, not a Basic Auth form.
  An anonymous browser is sent to sign in and back; a signed-in one sees a
  consent page naming the requesting host (never the name an app gives
  itself), the redirect host, the account, and that the app can read and send
  messages on every linked number. The subject on codes and tokens is the WTF
  user id. Allow needs the page's CSRF token and an explicit decision.
- **Clients: CIMD only, from `WTF_MCP_CLIENT_HOSTS`** (default `claude.ai`).
  Dynamic client registration is off (`/oauth/register` does not exist and is
  not advertised). A client id is the HTTPS URL of a client ID metadata
  document on an allowed host; it is fetched with GET only, through the
  outbound guard (public addresses only, checked at connect time), with no
  redirects, a 64 KiB cap read as a stream, a 10 second timeout and an
  `application/json` check; its `client_id` must name its own URL. Good
  documents are cached for 5 minutes, failures not at all. Redirects must
  match a declared one on scheme, host, path and query; for `localhost`,
  `127.0.0.1` and `[::1]` the port is ignored (Claude Code declares
  `http://localhost/callback` and listens on a random port), and `localhost`
  never matches `127.0.0.1`. The consent page's `form-action` CSP allows that
  redirect's origin only.
- **No Basic, no cookies on `/mcp`.** `/mcp` accepts an OAuth access token for
  this resource, or a personal API key (`Authorization: Bearer wtfk_...`, for
  Claude Code without OAuth). Anything else is a 401 with the
  `resource_metadata` challenge, before the body or protocol version is read.
  The person is re-read on every call: disabling them, revoking the key or
  disconnecting the app takes effect on the next call.
- **Devices.** `X-Device-Id` and a per-call `device_id` resolve only the
  caller's own numbers (another person's reads like an unknown one); with
  exactly one number it is the default. The `wtf_my_devices` tool lists them.
- **Disconnecting.** Account › Connected apps lists each connection (one per
  token family) with Disconnect, which revokes every token of that
  connection at once.

Allowing another vendor's app means adding its document host to
`WTF_MCP_CLIENT_HOSTS`, once that app is known to use CIMD (apps that only do
dynamic registration, such as Grok, cannot connect in this mode).
