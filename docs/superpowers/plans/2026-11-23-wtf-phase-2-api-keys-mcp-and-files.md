# WTF Phase 2: API Keys, Per-User MCP and Owner-Checked Files Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Every task is written to be cut straight into a builder brief (`captain-and-crew/brief-template.md`): it names its kind, model, files, interfaces, tests by layer, dependencies and what it must not touch.

**Goal:** Let each signed-in person reach their own WhatsApp from code and from Claude: personal API keys for the REST API, the MCP endpoint back on with per-user OAuth (WTF users as subjects, CIMD, one-click "Add to Claude"), and owner-checked fetching of downloaded media. Nothing a person reaches this way may belong to another person.

**Architecture:** Phase 1 left MCP off and `/statics` unmounted. This phase adds three entry points that all end in the same place: a `*store.User` in the request context (`auth.WithUser`), after which the existing owner resolver (`owners.Resolver`) and `owners.ScopedDevices` do the scoping exactly as they do for cookie sessions. API keys are a second way to load the user on the REST API only. The upstream OAuth server in `src/ui/mcp/oauth/` gains a small, upstream-shaped "session subject" mode (the browser's WTF session replaces the Basic Auth form) and CIMD; the MCP route gains a context-aware device resolver hook. A new outbound guard (`src/multiuser/netguard`) refuses non-public addresses at dial time for every server-side fetch a user can steer: send-by-URL, per-device webhooks and CIMD.

**Tech Stack:** as phase 1 (Go 1.26+, Fiber v3.4, mattn/go-sqlite3, whatsmeow, `html/template` + htmx 2.0.4, testify) plus `mark3labs/mcp-go` v0.56.0 (already a dependency) and `gofiber/fiber/v3/middleware/adaptor`'s `HTTPHandlerWithContext` / `LocalContextFromHTTPRequest`.

**Spec:** the design doc "GOWA multi-user fork: design" at https://claude.ai/code/artifact/24e3a973-7165-4898-a922-a8edb24b9824. **It could not be read when this plan was written** (the artifact read returned "not found" for this account on 2026-11-23). This plan is built from the phase 1 plan's references to phase 2 (Global Constraints, "Deviations" 3 and 5, Self-review notes), the code on `claude/epic-dijkstra-w0pkk1`, and the skills listed below. Executors who can open the spec must read its phase 2 section first and report any difference to the captain before building.

**Skills every builder loads first:** superpowers:test-driven-development together with `layering-tests-by-what-they-check`; `building-multitenant-saas-auth`; `securing-multitenant-saas`; for Tasks 5 to 8 also `securing-mcp-connector-oauth` and `mcp-oauth-for-claude-connectors`; for every UI task `frontend-house-style` and `ui-ux-checklist`, and read `docs/ui.md`.

## Global Constraints

- Everything in phase 1's Global Constraints still holds (module path, `// WTF:` markers on upstream files, upstream behaviour unchanged with `WTF_MULTIUSER` off, superadmins from the env allowlist only, never log a token, a key, a session id or message text, HTML for pages and JSON for machine routes, commit after every task, never push upstream tags).
- **One principal per request.** A request is authenticated by exactly one of: session cookie (pages and REST), API key (REST and `/files` only), OAuth access token (`/mcp` only), API key on `/mcp` (Claude Code convenience). A request that presents an `Authorization` header that fails is a 401; it never falls back to the cookie.
- **The user is re-checked on every request.** Keys and OAuth tokens resolve to a user id; the user row is read each time and anything but `active` is refused. Revoking a user, a key or a grant takes effect on the next request.
- **Keys and tokens are opaque and hashed.** API keys are `wtfk_` + `store.NewToken()` (43 chars base64url). Only `store.HashToken(raw)` is stored. The raw key is shown once. The list shows the name, the first 6 characters after the prefix, created and last used.
- **Scoping lives in the service layer.** No new route does its own ownership check by hand. It loads the user, then goes through `owners.Resolver.Resolve` or `owners.ScopedDevices`. Another user's device is a 404, never a 403.
- **Every server-side fetch a user can steer goes through `netguard`** in multi-user mode: HTTPS or HTTP to a globally routable address only, checked in the dialer's `Control` (so DNS rebinding cannot slip past), no redirects to a non-public address, bodies capped while streaming.
- **MCP is on in multi-user mode only through OAuth or an API key.** Basic Auth on `/mcp` stays off (`APP_BASIC_AUTH` is already cleared by `multiuser.Apply`). Dynamic client registration is off in multi-user mode; clients identify by CIMD from an allowlisted host (default `claude.ai`).
- **Accounts store migrations are append-only and added in Task 1 only.** No other task in this phase appends to `migrations` in `src/multiuser/store/store.go`. That keeps parallel tasks from colliding on the slice.
- **UI uses the design system on `claude/epic-dijkstra-w0pkk1`** (`docs/ui.md`): `.page-head`, `.card`, `.rows` > `.row`, `.pill`, `.btn` variants, `{{icon "name"}}` (Lucide only, add the SVG to `icons/` if missing), `{{template "confirm" ...}}` for destructive actions, `{{template "sheet" ...}}` for htmx-filled dialogs. No inline `style=`, no inline script, nothing from a CDN. Every new page and dialog state gets a fixture in `preview_test.go`.
- **The admin "Add user" change in flight on `ui/redesign` (`/home/user/wt-design`, uncommitted on 2026-11-23) touches `web.go`, `admin.go`, `account.html`, `layout.html` and `preview_test.go`.** UI tasks here start only after it has merged into the base branch, and rebase onto it.

## Review Focus

1. **The consent page's `form-action`.** WTF's page CSP is `form-action 'self'` (`src/multiuser/web/web.go:84`). The consent form posts to itself and the handler 302s to `https://claude.ai/api/mcp/auth_callback`; browsers apply `form-action` to that redirect, so Approve silently does nothing. The consent response must widen `form-action` to the origin of the *validated* redirect URI only. Only a real browser shows this; pinned in Task 6 Step 6 and Task 11 Step 4.
2. **Claude Code's loopback redirect.** Claude Code's CIMD declares `http://localhost/callback` and `http://127.0.0.1/callback` with no port and arrives on an ephemeral port. Matching ignores the port, and only the port, for loopback hosts, and never turns `localhost` into `127.0.0.1`. The upstream matcher refuses a port change on `localhost`; the CIMD path must not. Pinned in Task 6.
3. **A disabled or deleted user with a live API key or OAuth token.** Refused on the very next request on REST, `/files` and `/mcp`. Pinned in Tasks 3, 7 and 10.
4. **An API key on a page route, or a cookie plus a bad bearer.** Keys never sign anyone in to `/`, `/account` or `/admin/*`; a bad bearer is a 401 even with a good cookie. Pinned in Task 3.
5. **Path traversal and type confusion on `/files`.** `..`, encoded `%2e%2e`, absolute paths, symlinks out of the device's directory and another device's id all 404. Files are served `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff` and `Content-Security-Policy: sandbox`. Pinned in Task 4.
6. **SSRF through send-by-URL, per-device webhooks and CIMD.** `http://169.254.169.254/`, `http://127.0.0.1:3000/`, `http://[::1]/`, a hostname that resolves to `10.x`, and a public URL that redirects to any of those are all refused. Pinned in Task 2.
7. **Nothing answers non-401 before the MCP auth check.** A malformed body with no token, and an unsupported `MCP-Protocol-Version` with no token, both get 401 with `WWW-Authenticate`. Pinned in Task 7.

---

## File Structure

```
src/multiuser/
  config.go                  + MCPEnabled, MCPClientHosts, MCPResourceURL() (Task 0)
  store/
    store.go                 + migrations: api_keys (Task 1)
    apikeys.go               api_keys table: create, list, revoke, lookup, touch (Task 1)
    apikeys_test.go
  netguard/
    netguard.go              Dialer Control refusing non-global IPs; Client(); CheckURL() (Task 2)
    netguard_test.go
  auth/
    apikey.go                Manager.APIKey(): bearer key loading for REST and /files (Task 3)
    apikey_test.go
  files/
    files.go                 GET /files/:device_id/* owner-checked media (Task 4)
    files_test.go
  mcpauth/
    mcpauth.go               OAuth session-subject adapter (consent, subject, active check) + /mcp auth middleware (Tasks 5, 7)
    consent.go               consent page rendering through web templates (Task 5)
    mcpauth_test.go
  web/
    account.go               + API keys card handlers (Task 9), + Connected apps (Task 10)
    templates/account.html   + API keys card, + Connected apps card
    templates/apikey_new.html      dialog body: name form, then the key shown once
    templates/connect_claude.html  dialog body: Add to Claude, MCP URL, Claude Code command
    templates/consent.html   OAuth consent (signed-in shell-less card) (Task 5)
    icons/{plug.svg,terminal.svg,external-link.svg}  (Lucide, as needed)
    preview_test.go          + fixtures for every new state
  runtime.go                 wiring (Task 8 only)
src/ui/mcp/
  route.go                   // WTF: HTTPHandlerWithContext + Deps.ResolveDevice hook (Task 7)
  device.go                  // WTF: resolveDeviceContext uses the hook when set (Task 7)
  server.go                  // WTF: Deps.ExtraTools hook for wtf_my_devices (Task 7)
  oauth/
    types.go                 + Config.Session (SessionAuthenticator), Config.CIMD, Config.DisableDCR (Tasks 5, 6)
    server.go                // WTF-free, upstream-shaped: session mode in authorize; CIMD client lookup; metadata flag
    session.go               SessionAuthenticator interface and the session-mode authorize handlers (Task 5)
    cimd.go                  client_id metadata document fetch, validation, cache (Task 6)
    cimd_test.go
    store.go                 + RevokeSubject, ListGrants(subject), RevokeGrant(subject, family) (Task 5)
src/cmd/
  rest.go                    multi-user branch of MCP registration (Task 8)
  mcp_oauth.go               registerMcpOAuthMultiUser (Task 8)
  multiuser_isolation_test.go  + API key, /files and /mcp cross-user cases; new public routes (Task 11)
src/pkg/utils/general.go     // WTF: OutboundClient hook for the five Download*FromURL helpers (Task 2)
src/infrastructure/whatsapp/webhook.go  // WTF: OutboundClient hook for webhook delivery (Task 2)
src/usecase/message.go       // WTF: MediaRootHook so downloads land under statics/media/<device_id>/ (Task 4)
src/domains/message/message.go  + FileURL `json:"file_url,omitempty"` (Task 4)
src/validations/*            // WTF: webhook URL checked with netguard.CheckURL at save in multi-user mode (Task 2)
docs/mcp-oauth.md            + "Multi-user mode (WTF)" section (Task 12)
readme.md                    + new WTF_* variables (Task 12)
render.yaml                  + WTF_MCP_ENABLED (Task 12)
```

Upstream files touched (hooks only, each marked `// WTF:`): `ui/mcp/route.go`, `ui/mcp/device.go`, `ui/mcp/server.go`, `pkg/utils/general.go`, `infrastructure/whatsapp/webhook.go`, `usecase/message.go`, `domains/message/message.go`, the webhook validation file. The `ui/mcp/oauth/*` changes are written as general features (session mode, CIMD, DCR toggle) with no WTF imports, so they could be offered upstream.

---

## Task order and parallelism

```
T0 config ──► T1 store ──► T3 API-key auth ──► T9 UI: API keys ──► T10 UI: Connect Claude ─┐
     │                          │                                        ▲                  │
     ├──► T4 /files ────────────┼────────────────────────────────────────┼──────────────────┤
     │                          ▼                                        │                  ▼
     └──► T5 OAuth session ──► T6 CIMD ──► T7 MCP per user ──► T8 wiring ┘          T11 isolation ─► T12 docs + go-live
T2 netguard (no deps) ─────────► T6, T8
```

| Group | Tasks | Can run in parallel with | Why |
|---|---|---|---|
| A (start) | T0, then T1 | T2 | T0 and T1 own `config.go` and `store.go`; T2 touches neither |
| B | T2 | T0, T1, T3, T4, T5 | owns `netguard/` and the two upstream outbound hooks |
| C | T3, T4, T5 | each other, and T2 | disjoint files: `auth/apikey.go`; `files/` + `usecase/message.go`; `ui/mcp/oauth/session.go`, `store.go`, `types.go` |
| D | T6 | T3, T4, T9 | after T5 (shares `oauth/server.go`, `types.go`) and T2 (uses `netguard`) |
| E | T7 | T9, T4 | after T3 and T6; owns `ui/mcp/route.go`, `device.go`, `server.go`, `multiuser/mcpauth/mcpauth.go` |
| F | T9 then T10 | backend tasks | both edit `account.go`, `account.html`, `preview_test.go`; sequential |
| G | T8, T11, T12 | nothing | wiring touches `runtime.go`, `cmd/rest.go`, `cmd/mcp_oauth.go`; strictly last |

**Sizing:** 13 tasks (T0 to T12). Complexity: T0 S, T1 S, T2 M, T3 M, T4 M, T5 L, T6 L, T7 L, T8 M, T9 M, T10 M, T11 M, T12 S (plus a live connect check with Aryeh). Highest risk: T6 (CIMD and redirect matching), T5 (consent and CSP), T7 (per-user MCP scoping and auth ordering), T2 (an SSRF guard that must actually be on the path it guards).

**Models:** opus for T2, T3, T4, T5, T6, T7, T8, T11 (authorisation, tenant scoping, outbound fetches). sonnet for T0, T1, T9, T10, T12.

---

### Task 0: Settings for MCP in multi-user mode

**Kind:** backend. **Model:** sonnet. **Depends on:** nothing. **Parallel:** with T2.

**Files:**
- Modify: `src/multiuser/config.go`, `src/multiuser/config_test.go`

**Interfaces:**
- Produces:
  - `Settings.MCPEnabled bool` from `WTF_MCP_ENABLED`, default `true` when multi-user is on.
  - `Settings.MCPClientHosts []string` from `WTF_MCP_CLIENT_HOSTS` (comma-separated hostnames), default `["claude.ai"]`. Lower-cased, trimmed, no scheme, no port, no wildcards (refuse at load).
  - `(Settings) MCPResourceURL() string` = `PublicURL + "/mcp"`. The one place the issuer, resource and connect link all derive from.
  - `Apply()` no longer forces `config.McpEnabled`/`config.McpOAuthEnabled` off; it sets them from `Settings` via a new `ApplyMCP(s Settings)` called from `restServer`, and still clears `AppBasicAuthCredential`, so the Basic fallback on `/mcp` stays unreachable.

- [ ] **Step 1: Failing tests (behaviour).** `TestMCPDefaultsOnInMultiUser`, `TestMCPCanBeTurnedOff` (`WTF_MCP_ENABLED=false`), `TestMCPClientHostsParsed` (`" Claude.AI , chatgpt.com "` → `["claude.ai","chatgpt.com"]`), `TestMCPClientHostsRefusesWildcardsAndSchemes` (`*.claude.ai`, `https://claude.ai`, `claude.ai:443` each make `Load` fail), `TestMCPResourceURLDerivesFromPublicURL` (`https://wtf.example.com/` → `https://wtf.example.com/mcp`), `TestApplyMCPNeverEnablesBasicAuth`.
- [ ] **Step 2: Implement.** Keep `Load` pure (no globals). `ApplyMCP` sets `config.McpEnabled = s.MCPEnabled`, `config.McpOAuthEnabled = s.MCPEnabled`, `config.McpOAuthIssuerURL = s.PublicURL`, `config.McpOAuthResourceURL = s.MCPResourceURL()`.
- [ ] **Step 3: Break-check.** Comment out the wildcard refusal; `TestMCPClientHostsRefusesWildcardsAndSchemes` goes red. Restore.
- [ ] **Step 4: Commit.** `git add src/multiuser/config.go src/multiuser/config_test.go && git commit -m "feat(multiuser): settings for per-user MCP"`

---

### Task 1: Accounts store: API keys

**Kind:** backend. **Model:** sonnet. **Depends on:** T0 merged (only to keep one config/store chain). **Parallel:** with T2.

**Files:**
- Modify: `src/multiuser/store/store.go` (append two migrations)
- Create: `src/multiuser/store/apikeys.go`, `src/multiuser/store/apikeys_test.go`

**Interfaces:**
- Migrations appended (never edit shipped entries):
  ```sql
  CREATE TABLE api_keys (
      id TEXT PRIMARY KEY,                 -- newID("k")
      user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      key_hash TEXT NOT NULL UNIQUE,
      prefix TEXT NOT NULL,                -- first 6 chars after "wtfk_", for display
      name TEXT NOT NULL,
      created_at TIMESTAMP NOT NULL,
      last_used_at TIMESTAMP NULL,
      revoked_at TIMESTAMP NULL)
  CREATE INDEX api_keys_user ON api_keys(user_id)
  ```
- Produces:
  - `const APIKeyPrefix = "wtfk_"`; `const MaxAPIKeysPerUser = 10`
  - `type APIKey struct{ ID, UserID, Prefix, Name string; CreatedAt time.Time; LastUsedAt *time.Time }`
  - `(*Store).CreateAPIKey(ctx, userID, name string) (raw string, k *APIKey, err error)`: refuses an empty or over-60-character name, and an 11th live key (`ErrTooManyKeys`). Count and insert in one statement, like `ReserveOwner`.
  - `(*Store).APIKeysOf(ctx, userID) ([]APIKey, error)`: live keys, newest first.
  - `(*Store).RevokeAPIKey(ctx, userID, keyID string) error`: `ErrNotFound` unless the key is the user's and live. Another user's key id is `ErrNotFound`.
  - `(*Store).UserByAPIKey(ctx, raw string) (*User, string /*keyID*/, error)`: hash, join users, live keys only; `ErrNotFound` for anything else, including a raw value without the prefix.
  - `(*Store).TouchAPIKey(ctx, keyID string) error`: sets `last_used_at` only when it is null or older than 5 minutes (one write per key per 5 minutes, not per request).
  - `(*Store).RevokeAllAPIKeys(ctx, userID) error` (used by phase 3's Delete my account and by the admin disable path).

- [ ] **Step 1: Failing tests.** Behaviour: `TestAPIKeyRoundTrip` (create, then `UserByAPIKey(raw)` returns the user and key id), `TestAPIKeyStoredHashedOnly` (scan the row: no column contains `raw`), `TestRevokedKeyNoLongerAuthenticates`, `TestRevokeAnotherUsersKeyIsNotFound`, `TestAPIKeysOfListsOnlyMine`, `TestKeyNameRequired`, `TestTouchIsThrottled` (frozen `Now`: two touches 1 minute apart write once, 6 minutes apart write twice). Mechanism: `TestKeyLimitHoldsUnderConcurrency` (20 goroutines creating keys for one user against one file DB end with exactly 10 live keys; prove it can fail by temporarily splitting count and insert into two statements).
- [ ] **Step 2: Implement** with parameterised SQL only; `NewToken`, `HashToken`, `newID` from `tokens.go`.
- [ ] **Step 3: Run** `cd src && go test ./multiuser/store/... -count=1`. Expected PASS.
- [ ] **Step 4: Commit.** `git add src/multiuser/store/store.go src/multiuser/store/apikeys.go src/multiuser/store/apikeys_test.go && git commit -m "feat(store): personal API keys"`

---

### Task 2: Outbound guard for every fetch a user can steer

**Kind:** backend (security). **Model:** opus. **Depends on:** nothing. **Parallel:** with T0, T1, T3, T4, T5.

**Why now:** with API keys and MCP, people who are not superadmins can make the server fetch URLs (`image_url`, `file_url`, `video_url`, `audio_url`, `sticker_url` on the send routes and tools) and can set per-device webhook URLs (`ScopedDevices.SetDeviceWebhook*`). Today none of these paths checks the destination (`src/pkg/utils/general.go:174,348,404,495,611`, `src/infrastructure/whatsapp/webhook.go`). On Render that reaches the private network and the instance's own port.

**Files:**
- Create: `src/multiuser/netguard/netguard.go`, `src/multiuser/netguard/netguard_test.go`
- Modify (hooks only, `// WTF:`): `src/pkg/utils/general.go`, `src/infrastructure/whatsapp/webhook.go`, the per-device webhook validation in `src/validations/` (find it with `grep -rn "webhook" src/validations`)
- Modify: `src/multiuser/runtime.go` is **not** touched here; T8 installs the hooks.

**Interfaces:**
- Produces:
  - `netguard.ErrRefused` (wraps a reason: "not https or http", "address not public", "too many redirects", "body too large")
  - `netguard.Allowed(ip netip.Addr) bool`: false for loopback, private (RFC 1918, fc00::/7), link-local (169.254/16, fe80::/10), CGNAT 100.64/10, unspecified, multicast, broadcast, 0/8, documentation ranges, IPv4-mapped forms of any of these, and NAT64 `64:ff9b::/96` mapping a refused v4.
  - `netguard.Dialer() *net.Dialer` with `Control` that parses the *connecting* address and refuses it (this is what defeats DNS rebinding, because it judges the address actually dialled).
  - `netguard.Client(timeout time.Duration, maxRedirects int) *http.Client`: transport uses `Dialer()`, `Proxy: nil` (a proxy would hide the real destination), redirects capped and each hop's scheme re-checked.
  - `netguard.CheckURL(raw string) error`: save-time check (scheme http/https, host present, every resolved address `Allowed`). Advisory only; the dialer is the real guard.
  - `netguard.LimitBody(r io.Reader, max int64) io.Reader` that errors past `max`.
- Upstream hooks:
  - `pkg/utils/general.go`: `var OutboundClient func(timeout time.Duration) *http.Client // WTF: set in multi-user mode`; each `Download*FromURL` uses it when non-nil, else its current client.
  - `infrastructure/whatsapp/webhook.go`: same `OutboundClient` hook for delivery.
  - Webhook save validation: `var WebhookURLCheck func(string) error // WTF:`; called when non-nil.

- [ ] **Step 1: Failing tests.**
  - Behaviour (`netguard_test.go`): a table over `Allowed` for every refused range above plus `8.8.8.8`, `1.1.1.1`, `2606:4700::1111` allowed; `TestCheckURLRefusesNonHTTP` (`file:///etc/passwd`, `gopher://`, `ftp://`).
  - Mechanism (the guard is on the dial path, not beside it): `TestClientRefusesLoopbackAtDial` (an `httptest.Server` on 127.0.0.1; `Client().Get(srv.URL)` fails with `ErrRefused`); `TestClientRefusesRedirectToLoopback` (a server reachable through a test-only `Allowed` override for one address, redirecting to `http://127.0.0.1/`, is refused on the hop); `TestRebindingIsJudgedAtConnect` (a custom resolver returns a public address to `CheckURL` and `127.0.0.1` to the dialer; the fetch is refused). Use a package-level `testAllow func(netip.Addr) bool` seam, unexported, only for these tests.
  - Security (names its caller): `TestDownloadFromURLUsesTheGuardWhenSet` in `pkg/utils` (set `OutboundClient` to a client that records calls; `DownloadImageFromURL` goes through it) and `TestWebhookDeliveryUsesTheGuardWhenSet` in `infrastructure/whatsapp`.
  - Behaviour, body cap: `TestLimitBodyStopsMidStream` (a server streaming 10 MB, cap 64 KiB, read stops near 64 KiB; assert bytes read, not only the error).
- [ ] **Step 2: Implement `netguard`.** Use `net/netip` prefixes; unwrap `Is4In6`. In `Control`, `netip.ParseAddrPort(address)`, refuse if `!Allowed`. `CheckRedirect` returns an error after `maxRedirects` and on a non-http(s) scheme.
- [ ] **Step 3: Add the three hooks** with `// WTF:` comments naming this task. Upstream behaviour is unchanged when the vars are nil (assert with the existing tests in those packages: `go test ./pkg/utils/... ./infrastructure/whatsapp/... -count=1`).
- [ ] **Step 4: Break-check.** Make `Control` return nil; `TestClientRefusesLoopbackAtDial` and `TestRebindingIsJudgedAtConnect` go red. Restore.
- [ ] **Step 5: Commit.** Stage the new package and the three hooked files by name. `git commit -m "feat(multiuser): outbound guard for user-steered fetches"`

---

### Task 3: API-key authentication on the REST API

**Kind:** backend (security). **Model:** opus. **Depends on:** T1. **Parallel:** with T2, T4, T5.

**Files:**
- Create: `src/multiuser/auth/apikey.go`, `src/multiuser/auth/apikey_test.go`
- Modify: `src/multiuser/auth/auth.go` only if a small unexported helper is needed (for example to clear the cookie principal); keep the diff minimal and say so in the commit.

**Interfaces:**
- Produces:
  - `(*Manager).APIKey() fiber.Handler`. Mounted by T8 at the head of the API gate chain: `app.Use(r.Auth.APIKey(), r.Auth.RequireAPI(), r.Auth.RequireSameOriginAPI(), r.Auth.VerifyCSRF())`.
  - Behaviour:
    - No `Authorization` header: pass through untouched (cookie path as today).
    - `Authorization: Bearer wtfk_...`: look up with `UserByAPIKey`. Not found, revoked, or user not active: JSON 401 `{"code":"INVALID_API_KEY"}` with `WWW-Authenticate: Bearer realm="wtf"`. Found: **drop the cookie principal** (`keyRaw` to "", user replaced), set `WithUser(ctx, u)`, set an unexported `keyVia = "api_key"` local, `TouchAPIKey` asynchronously-safe (in the request, it is throttled to one write per 5 minutes).
    - Any other `Authorization` scheme or a bearer without the `wtfk_` prefix on the REST API: pass through (the OAuth bearer is only meaningful on `/mcp`, which T7 guards separately; the REST gate then refuses it as anonymous unless a cookie is present). **Decision recorded:** a non-WTF bearer with a valid cookie still works as the cookie, because browsers never send one; a `wtfk_` bearer that fails is always 401.
  - Because `keyRaw` is empty for key requests, `RequireSameOriginAPI` and `VerifyCSRF` pass them (they judge cookie sessions only). Document this at the call site.
  - `(*Manager).AuthenticatedBy(c) string` returns `"session"`, `"api_key"` or `""` (for audit lines and tests).

- [ ] **Step 1: Failing tests (`apikey_test.go`, a bare fiber app with `Middleware()`, `APIKey()`, the three gates and a `GET /whoami` that echoes `UserFrom`).**
  - Behaviour: `TestValidKeyAuthenticates`; `TestKeyRequestNeedsNoCSRF` (POST with a key and no token: 200); `TestRevokedKeyIs401`; `TestDisabledUsersKeyIs401` (create, disable with `SetUserStatus`, next request 401); `TestUnknownWTFKeyIs401EvenWithAGoodCookie`; `TestKeyWinsOverCookie` (cookie for Alice, key for Bob: whoami is Bob, and the response sets no new session cookie).
  - Security: `TestKeyNeverOpensAPage` (mount `web.Mount` as `Install` does, GET `/account` with only a key: 303 to `/signin`); `TestKeyIsNotLogged` (capture logrus output during a failing and a passing request; the raw key never appears).
- [ ] **Step 2: Implement.** Constant-time is not needed for a hash lookup by primary key; do not compare raw keys in Go.
- [ ] **Step 3: Break-check.** Remove the user-status check: `TestDisabledUsersKeyIs401` red. Make a failing key fall through: `TestUnknownWTFKeyIs401EvenWithAGoodCookie` red. Restore both.
- [ ] **Step 4: Commit.** `git commit -m "feat(auth): personal API keys on the REST API"` (stage files by name).

---

### Task 4: Owner-checked media fetching

**Kind:** backend (security). **Model:** opus. **Depends on:** T0. **Parallel:** with T2, T3, T5.

**Files:**
- Create: `src/multiuser/files/files.go`, `src/multiuser/files/files_test.go`
- Modify (hook): `src/usecase/message.go` (around line 639, the media download), `src/usecase/newsletter.go` (around line 123), `src/domains/message/message.go` (`FileURL`)

**Interfaces:**
- Upstream hooks:
  - `usecase`: `var MediaRootHook func(ctx context.Context) (dir string, urlPrefix string, ok bool) // WTF:`. When set and ok, downloads write under `dir` instead of `config.PathMedia`, and the response's new `FileURL` is `urlPrefix + "/" + <path relative to dir>` (slash-separated, each segment `url.PathEscape`d).
  - `domains/message.DownloadMediaResponse` (find the struct with `FilePath`): add `FileURL string \`json:"file_url,omitempty"\``. Upstream responses are unchanged (field omitted).
- Produces:
  - `files.Deps{Resolver interface{ Resolve(ctx, id string) (*whatsapp.DeviceInstance, string, error) }; Root string /* "statics/media" */}`
  - `files.MediaRoot(root string) func(ctx) (string, string, bool)`: reads the device from context (`whatsapp.DeviceFromContext` or whatever `usecase` already uses to pick the device), returns `(<root>/<instance.ID()>, "/files/<instance.ID()>", true)`.
  - `files.Mount(r fiber.Router, d Deps)`: `GET /files/:device_id/*`. Resolves the device through `Resolver.Resolve(c.Context(), deviceID)` (owner check; not found or not owned: 404 JSON), then serves `<Root>/<resolvedID>/<rest>` after:
    - rejecting any segment equal to `..` or `.` after percent-decoding, any backslash, NUL, or a leading `/` in the remainder;
    - `filepath.EvalSymlinks` on the final path and requiring it to stay under `EvalSymlinks(<Root>/<resolvedID>)`;
    - regular files only.
  - Response headers: `Content-Type` from the extension with a fixed allowlist (jpeg, png, webp, gif, mp4, ogg/opus, mp3, m4a, pdf, else `application/octet-stream`), `Content-Disposition: attachment; filename*=UTF-8''<escaped base>`, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox; default-src 'none'`, `Cache-Control: private, no-store`.
  - Reachable with a cookie session or an API key (T8 mounts it after the API gate). Not reachable over MCP (MCP returns `file_url` and the client fetches it with its key).

- [ ] **Step 1: Failing tests.**
  - Behaviour: `TestOwnFileIsServed`; `TestDownloadWritesUnderTheDeviceDir` (call the usecase's download path with `MediaRootHook` set and a fake extracted file; assert the file lands under `statics/media/<id>/` and `file_url` is `/files/<id>/...`); `TestHeadersMakeItADownload`.
  - Security, table-driven `TestTraversalIs404`: `../d_bob/x.jpg`, `%2e%2e/d_bob/x.jpg`, `..%2fd_bob%2fx.jpg`, `/etc/passwd`, `a\\..\\b`, `x%00.jpg`, a symlink inside Alice's dir pointing into Bob's dir. `TestAnotherUsersDeviceIs404` (Alice's session or key, Bob's device id, a file that exists: 404, and the body does not differ from a missing-file 404).
  - Shape: `TestMediaRootHookIsNilUpstream` (with `config.MultiUserEnabled=false` the response JSON has no `file_url`).
- [ ] **Step 2: Implement.** Never build the path with string concatenation; `filepath.Join` then the containment check.
- [ ] **Step 3: Break-check.** Remove the `EvalSymlinks` containment check: the symlink row goes red. Restore.
- [ ] **Step 4: Commit.** `git commit -m "feat(multiuser): owner-checked media fetching"`

---

### Task 5: OAuth server: session-subject mode (WTF users as OAuth subjects)

**Kind:** backend (security). **Model:** opus. **Depends on:** T0. **Parallel:** with T2, T3, T4.

Upstream's authorize page asks for a Basic Auth username and password and uses the username as the subject (`ui/mcp/oauth/server.go:386`). In multi-user mode there are no such credentials. This task adds an upstream-shaped option: the authorization server asks a `SessionAuthenticator` who the browser is signed in as, sends anonymous browsers to sign in first, shows a consent page, and uses the returned subject (the WTF user id, never the phone number) on codes and tokens.

**Files:**
- Create: `src/ui/mcp/oauth/session.go`, `src/ui/mcp/oauth/session_test.go`
- Modify: `src/ui/mcp/oauth/types.go`, `src/ui/mcp/oauth/server.go` (branch at `New`, `authorizeGET`, `authorizePOST`; no behaviour change when `Config.Session` is nil), `src/ui/mcp/oauth/store.go` (grant listing and revocation)
- Create: `src/multiuser/mcpauth/mcpauth.go` (the WTF implementation of the interface), `src/multiuser/mcpauth/consent.go`, `src/multiuser/mcpauth/mcpauth_test.go`
- Create: `src/multiuser/web/templates/consent.html`; export a narrow renderer from `web` (`web.RenderStandalone(c, d, name, title string, data any, extraCSP string) error`) in a new file `src/multiuser/web/standalone.go` so `web.go` is not edited.

**Interfaces:**
- `oauth.SessionAuthenticator`:
  ```go
  type SessionAuthenticator interface {
      // Subject is the signed-in, active user's stable id, or ok=false.
      Subject(c fiber.Ctx) (subject string, ok bool)
      // SignInURL is where to send an anonymous browser; next is the full authorize URL to return to.
      SignInURL(next string) string
      // CSRFToken/VerifyCSRF bind the consent POST to the browser session.
      CSRFToken(c fiber.Ctx) string
      VerifyCSRF(c fiber.Ctx) bool
      // RenderConsent draws the consent page. formActionOrigin is the validated redirect origin (Review Focus 1).
      RenderConsent(c fiber.Ctx, v ConsentView, formActionOrigin string) error
      // Active reports whether subject may still use tokens (checked at token issue, refresh and every MCP call).
      Active(ctx context.Context, subject string) bool
  }
  type ConsentView struct {
      ClientHost   string // host of the client_id URL (CIMD) or "" for DCR clients
      ClientName   string // self-asserted; shown small, never as the relying party
      RedirectHost string
      LoopbackOnly bool   // every declared redirect is loopback: show the warning
      Hidden       map[string]string // the authorization request fields to echo back
      CSRF         string
  }
  ```
- `oauth.Config` gains `Session SessionAuthenticator` and `DisableDCR bool`. `New(cfg, validate)` accepts `validate == nil` when `cfg.Session != nil`.
- Session-mode flow:
  - `GET /oauth/authorize`: validate the request exactly as today (client, redirect, PKCE S256, `resource` **required** and equal to the configured resource, `response_type=code`). If `Subject` is not ok: 303 to `SignInURL(<this URL>)`. Else render consent.
  - `POST /oauth/authorize`: re-validate everything, then `VerifyCSRF`, then `Subject` (must still be signed in and active), then read `decision` (`approve` or `deny`, anything else or missing is a 400 page, never a default). Deny: redirect with `error=access_denied` and `iss`. Approve: issue the code with `Subject`.
  - `POST /oauth/token`: on both grants, after the existing checks, `Active(subject)` must be true, else `invalid_grant`.
- Store additions: `RevokeSubject(ctx, subject) error` (revokes every token of the subject and deletes unused codes), `ListGrants(ctx, subject) ([]Grant, error)` (one row per token family: client id, client display host, created, last refreshed), `RevokeGrant(ctx, subject, familyID) error` (`ErrInvalidGrant` when the family is not the subject's).
- WTF side (`mcpauth.Authenticator` implementing the interface): `Subject` reads `auth.UserFrom(c.Context())` (the session middleware is global, so it has already run) and returns `u.ID`; `SignInURL` returns `/signin?next=` + `url.QueryEscape(auth.SafeNext(next))` (the authorize URL is a local path because issuer and site share an origin); CSRF uses `auth.Manager.CSRFToken` and a constant-time compare of the form field `csrf`; `Active` reads `store.UserByID` and requires `StatusActive`; `RenderConsent` uses `web.RenderStandalone` with `extraCSP = "form-action 'self' " + formActionOrigin`.
- `consent.html` (UI, auth-card layout from `docs/ui.md`): product brand; heading "Allow access to your WhatsApp?"; the **client host** as the requester ("claude.ai wants to use your WTF account"), the client name in `.card-sub` below it; one sentence stating the authority: "It will be able to read and send messages on every WhatsApp number you've linked, until you disconnect it on your Account page."; a `.notice` warning when `LoopbackOnly` ("This app runs on this computer. Only continue if you started this from Claude Code."); two submit buttons `name="decision"` (`deny` secondary, `approve` primary) with the busy state built with a class and `aria-busy`, **never `disabled`** (the `mcp-oauth-for-claude-connectors` "Approve does nothing" trap; `app.js` busy handling must not apply `disabled` to these: give the form `data-no-disable` and make `app.js` honour it, or confirm it already defers by a tick).

- [ ] **Step 1: Failing tests in `oauth/session_test.go` (a fake `SessionAuthenticator`).**
  - Behaviour: `TestAnonymousAuthorizeRedirectsToSignIn` (303, `Location` carries the full authorize URL in `next`); `TestSignedInAuthorizeShowsConsent`; `TestApproveIssuesCodeForTheSessionSubject` (exchange it; the token's `Principal.Subject` is the fake's subject); `TestDenyRedirectsWithAccessDenied`; `TestMissingDecisionIsNotConsent` (400, no code issued: assert no row in `oauth_authorization_codes`); `TestConsentPostWithoutCSRFIs403`; `TestInactiveSubjectCannotRedeemOrRefresh` (approve, then flip the fake's `Active` to false; code exchange and refresh both `invalid_grant`); `TestResourceIsRequiredInSessionMode` (omitted `resource`: error, not a token valid everywhere).
  - Behaviour, store: `TestRevokeSubjectKillsEveryToken`, `TestListGrantsIsPerSubject`, `TestRevokeAnotherSubjectsGrantFails`.
  - Shape: `TestBasicModeUnchanged` (run the existing `server_test.go` and `security_test.go` unchanged; they must stay green), `TestDCRCanBeDisabled` (`DisableDCR`: `/oauth/register` answers 404 and metadata omits `registration_endpoint`).
- [ ] **Step 2: Failing tests in `multiuser/mcpauth/mcpauth_test.go`.** `TestSubjectIsUserIDNotPhone`; `TestDisabledUserIsNotActive`; `TestConsentCSPWidensFormActionToTheValidatedOriginOnly` (the response's CSP contains `form-action 'self' https://claude.ai` and nothing from the raw query); `TestConsentNamesTheClientHostNotTheName` (client name "Anthropic Official", host `evil.example`: the heading shows `evil.example`).
- [ ] **Step 3: Implement** `session.go`, the `server.go` branches, store additions, the WTF adapter and `consent.html`. Add the consent page and its loopback-warning and error states to `preview_test.go` fixtures **in T10**, not here (T10 owns `preview_test.go` changes for this phase's screens); leave a `// T10: preview fixture` note in the commit body.
- [ ] **Step 4: Break-check.** Default a missing `decision` to approve: `TestMissingDecisionIsNotConsent` red. Restore.
- [ ] **Step 5: Run** `cd src && go test ./ui/mcp/... ./multiuser/mcpauth/... -count=1`.
- [ ] **Step 6: Commit.** `git commit -m "feat(mcp-oauth): session-subject mode; WTF users as OAuth subjects"`

---

### Task 6: OAuth server: Client ID Metadata Documents (CIMD)

**Kind:** backend (security). **Model:** opus. **Depends on:** T2 (netguard), T5 (shares `server.go`, `types.go`). **Parallel:** with T3, T4, T9.

**Files:**
- Create: `src/ui/mcp/oauth/cimd.go`, `src/ui/mcp/oauth/cimd_test.go`
- Modify: `src/ui/mcp/oauth/types.go` (`Config.CIMD *CIMDConfig`), `src/ui/mcp/oauth/server.go` (client lookup in `validateAuthorizationRequest` and `token`; metadata flag; redirect matching for CIMD clients)

**Interfaces:**
- `type CIMDConfig struct { AllowedHosts []string; HTTPClient *http.Client; MaxBytes int64 /*default 16 KiB*/; CacheTTL time.Duration /*default 5m*/; UserAgent string }`. The `oauth` package does not import `netguard`; T8 passes `netguard.Client(...)` as `HTTPClient`. Upstream gets a safe default only if it supplies its own client, so `New` refuses `CIMD != nil && HTTPClient == nil`.
- A `client_id` that starts with `https://` is a CIMD client. Resolution:
  1. Parse; require `https`, no userinfo, no fragment, a non-root path, port empty or 443, and host in `AllowedHosts` (exact, case-insensitive). Else `invalid_client`/`invalid_request` page (the redirect is not trusted yet, so never redirect an error).
  2. `GET` only (Claude's endpoints 405 on `HEAD`), `Accept: application/json`, explicit `User-Agent` from config (default `"WTF-connector-check/1"`), **no redirects** (decision recorded: Claude's documents redirect zero times; refusing is simpler than per-hop re-checks).
  3. Require status 200 and `Content-Type` `application/json` (parameters allowed). Read through a limit of `MaxBytes+1` while streaming; over the cap is an error.
  4. Decode; require `client_id` == the URL exactly; `redirect_uris` non-empty, each valid; `token_endpoint_auth_method` absent or `none`; `grant_types` within `authorization_code`, `refresh_token`.
  5. Cache by URL for `CacheTTL` (success only; failures are not cached so a fixed vendor document is picked up at once). The cache is bounded (at most 32 entries; `AllowedHosts` already bounds it).
- Redirect matching for CIMD clients: exact on scheme, host, path and the **parsed** query (order-insensitive, extra pairs refused); for a loopback host (`localhost`, `127.0.0.1`, `[::1]`) the port is ignored, and only the port. `localhost` never matches `127.0.0.1`. Compare effective ports (443 for https, 80 for http) for non-loopback hosts.
- `ConsentView.ClientHost` = the CIMD URL's host; `LoopbackOnly` = every declared redirect is loopback.
- Authorization-server metadata adds `"client_id_metadata_document_supported": true` when CIMD is configured.
- The consent `formActionOrigin` is derived from the **request's** validated `redirect_uri` (scheme://host[:port]), after matching, per the skill.

- [ ] **Step 1: Failing tests (`cimd_test.go`, an `httptest.NewTLSServer` with its client passed as `HTTPClient`, and `AllowedHosts` set to its host).**
  - Behaviour: `TestClaudeDesktopDocumentAccepted` (the measured desktop document shape: redirect `https://claude.ai/api/mcp/auth_callback`, `token_endpoint_auth_method: none`, scopes `mcp:use offline_access`); `TestClaudeCodeEphemeralPortAccepted` (declared `http://localhost/callback`, requested `http://localhost:54321/callback`); `TestLocalhostDoesNotMatch127` ; `TestQueryOrderDoesNotMatterButExtrasDo`; `TestDefaultPortEquivalence` (declared `https://x/cb` matches `https://x:443/cb`).
  - Security: `TestHostNotAllowlistedRefusedWithoutFetching` (server counts requests: zero); `TestSelfReferenceRequired` (document `client_id` differs: refused); `TestRedirectNotFollowed`; `TestOversizeDocumentAbandonedMidStream` (server streams 1 MB; assert the server observed the client close early, or that bytes read ≤ cap+1 via a counting reader in the test server); `TestHTMLContentTypeRefused`; `TestHeadIsNeverUsed` (server 405s `HEAD`, test passes only if the GET path is used); `TestUnknownClientIsNotAReachableStateForCIMD` (no row in `oauth_clients` is ever needed or created for a CIMD client).
  - Behaviour, metadata: `TestMetadataAdvertisesCIMD`.
  - Mechanism: `TestCacheServesWithinTTLAndRefetchesAfter` (frozen clock).
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Break-check.** Remove the self-reference check: red. Change loopback matching to ignore host too: `TestLocalhostDoesNotMatch127` red. Restore.
- [ ] **Step 4: Commit.** `git commit -m "feat(mcp-oauth): client ID metadata documents"`

---

### Task 7: MCP per user (auth middleware, owner-scoped devices, my-devices tool)

**Kind:** backend (security). **Model:** opus. **Depends on:** T3 (API keys), T5, T6. **Parallel:** with T4, T9.

**Files:**
- Modify (hooks, `// WTF:`): `src/ui/mcp/route.go`, `src/ui/mcp/device.go`, `src/ui/mcp/server.go`
- Modify: `src/multiuser/mcpauth/mcpauth.go` (add the `/mcp` middleware), `src/multiuser/mcpauth/mcpauth_test.go`
- Create: `src/multiuser/mcpauth/tools.go`, `src/multiuser/mcpauth/tools_test.go`

**Interfaces:**
- `ui/mcp.Deps` gains:
  - `ResolveDevice func(ctx context.Context, deviceID string) (*whatsapp.DeviceInstance, string, error) // WTF: owner-checked in multi-user mode`
  - `ExtraTools func(s *server.MCPServer) // WTF: adds wtf_my_devices`
- `route.go`: when `deps.ResolveDevice` is set, mount with `adaptor.HTTPHandlerWithContext` and, in `WithHTTPContextFunc`, start from `adaptor.LocalContextFromHTTPRequest(r)` (which carries `auth.WithUser` set by the middleware), then resolve the `X-Device-Id` header with `deps.ResolveDevice(ctx, id)`. An empty header with exactly one owned device resolves it; zero or several leave the context without a device so tools answer "device identification required" (phase 1 Review Focus 3).
- `device.go`: `resolveDeviceContext` uses `deps.ResolveDevice(ctx, id)` for a per-call `device_id` argument when the hook is set. Another user's id: a tool error that reads the same as an unknown id.
- `mcpauth.Middleware(oauthSrv *oauth.Server, keys KeyLookup, auth *auth.Manager) fiber.Handler` for `/mcp`:
  1. Runs **first** on `/mcp`, before any body parsing, version check or rate limit (Review Focus 7).
  2. `Authorization: Bearer wtfk_...`: API key path as T3 (user must be active).
  3. Any other bearer: `oauthSrv.ValidateAccessToken(c)` (export a method from `server.go` that wraps `store.validateAccessToken` with the configured resource, added in this task), then `Active(subject)`; then `WithUser`.
  4. Otherwise, or on any failure: 401 with `WWW-Authenticate: Bearer resource_metadata="<protected-resource metadata URL>"` (reuse `oauthSrv`'s challenge builder) and no Basic advertisement.
  5. Cookies are ignored on `/mcp` (no CSRF exposure on a cookie-authenticated JSON-RPC endpoint).
- `wtf_my_devices` tool (read-only): lists the caller's devices as `{total, items:[{device_id, phone, label, state}]}` using `ScopedDevices.ListDevices` (label arrives in phase 3; omit the field until then). Description says to pass `device_id` to other tools when the person has more than one number.

- [ ] **Step 1: Failing tests.**
  - Security (`mcpauth_test.go`, a fiber app mounting the middleware in front of the real `uimcp.Register` with fakes for usecases): `TestNoTokenIs401WithResourceMetadata`; `TestMalformedBodyWithoutTokenIs401` and `TestUnsupportedProtocolVersionWithoutTokenIs401` (Review Focus 7); `TestCookieIsIgnoredOnMCP`; `TestDisabledUsersTokenIs401`; `TestRevokedGrantIs401`; `TestAPIKeyWorksOnMCP`.
  - Security (scoping): `TestHeaderDeviceOfAnotherUserIsNotResolved` (Alice's token, `X-Device-Id: d_bob`, call a read tool: tool error, and the fake usecase was never called with Bob's device); `TestToolArgumentDeviceOfAnotherUserRefused`; `TestSingleOwnedDeviceIsTheDefault`; `TestTwoOwnedDevicesNeedAnID`.
  - Behaviour (`tools_test.go`): `TestMyDevicesListsOnlyMine` with `total` equal to the items drawn.
  - Shape: `TestUpstreamMCPRouteUnchangedWithoutHook` (existing `ui/mcp` tests stay green; with `ResolveDevice` nil the route still uses `dm.ResolveDevice`).
- [ ] **Step 2: Implement** the hooks and middleware.
- [ ] **Step 3: Break-check.** Make the middleware fall back to the cookie: `TestCookieIsIgnoredOnMCP` red. Make the context func ignore the user and call `dm.ResolveDevice`: `TestHeaderDeviceOfAnotherUserIsNotResolved` red. Restore.
- [ ] **Step 4: Commit.** `git commit -m "feat(mcp): per-user MCP with owner-scoped devices"`

---

### Task 8: Wire phase 2 into multi-user startup

**Kind:** backend. **Model:** opus. **Depends on:** T2 to T7. **Parallel:** none (owns `runtime.go`, `cmd/rest.go`, `cmd/mcp_oauth.go`).

**Files:**
- Modify: `src/multiuser/runtime.go`, `src/cmd/rest.go`, `src/cmd/mcp_oauth.go`, `src/cmd/multiuser.go`, `src/cmd/rest_route_order_test.go`, `src/cmd/mcp_route_test.go`

**Steps:**
- [ ] **Step 1: Failing tests.** `TestMultiUserMountsMCPWithOAuthOnly` (in multi-user mode `/mcp` answers 401 with a Bearer challenge and no `Basic`); `TestMultiUserServesDiscoveryDocuments` (`/.well-known/oauth-protected-resource/mcp` and `/.well-known/oauth-authorization-server` are 200 and name `WTF_PUBLIC_URL`, not the request Host: send a forged `Host` and assert the documents do not change); `TestFilesMountedBehindTheAPIGate` (anonymous `/files/x/y` 401 JSON); `TestDeviceGroupIsRegisteredLast` stays green; `TestOutboundHooksInstalledInMultiUser` (`utils.OutboundClient`, the webhook hook and `WebhookURLCheck` are non-nil after `Setup`, and nil after `Close`).
- [ ] **Step 2: Implement.**
  - `multiuser.Setup`: build `mcpauth.Authenticator`; set `usecase.MediaRootHook = files.MediaRoot("statics/media")`, `utils.OutboundClient`, the webhook delivery hook and `WebhookURLCheck`; `Close` clears all of them (like `DeviceResolveFunc`).
  - `Runtime.Install`: after `web.Mount`, `app.Use(r.Auth.APIKey(), r.Auth.RequireAPI(), r.Auth.RequireSameOriginAPI(), r.Auth.VerifyCSRF())`, then `files.Mount(app, ...)`.
  - `cmd/rest.go`: call `multiuser.ApplyMCP(muSettings)` right after `Apply()`. `registerMcpOAuth` gets a multi-user branch (`registerMcpOAuthMultiUser(app, dm, rt)`): `oauth.New(Config{IssuerURL, ResourceURL, StorageURI, Session: rt.MCPAuth, DisableDCR: true, CIMD: &CIMDConfig{AllowedHosts: s.MCPClientHosts, HTTPClient: netguard.Client(10*time.Second, 0), UserAgent: s.ProductName + "-connector-check/1"}}, nil)`, `RegisterPublic(app)` **before** `rt.Install` gates anything (the well-known and token endpoints are public; authorize relies on the global session middleware, which `Install` mounts first, so order: `app.Use(Auth.Middleware())` must precede `RegisterPublic`). Restructure so `Install` is split into `InstallSession(app)` and `InstallGates(app)` if needed, and pin the final order in `rest_route_order_test.go`.
  - `/mcp` is mounted with `mcpauth.Middleware` and `uimcp.Register(..., Deps{..., ResolveDevice: rt.Resolver.Resolve, ExtraTools: mcpauth.Tools(rt.Devices)})`. It must not sit behind `RequireAPI`/`RequireSameOriginAPI`/`VerifyCSRF` (it has its own auth).
  - The CORS middleware's `oauthAuthorizePath` exemption keeps working because `config.McpOAuthIssuerURL` is now set by `ApplyMCP`.
- [ ] **Step 3: Run** `cd src && go test ./cmd/... ./multiuser/... ./ui/mcp/... -count=1`.
- [ ] **Step 4: Commit.** `git commit -m "feat(wtf): wire API keys, per-user MCP and file fetching"`

---

### Task 9: UI: API keys on the Account page

**Kind:** UI. **Model:** sonnet. **Depends on:** T1, T3 (backend), and the in-flight "Add user" redesign change merged. **Parallel:** with T4, T6, T7. **Sequential with:** T10 (same files).

**Files:**
- Modify: `src/multiuser/web/account.go`, `src/multiuser/web/templates/account.html`, `src/multiuser/web/preview_test.go`, `src/multiuser/web/account_test.go`
- Create: `src/multiuser/web/templates/apikey_new.html`
- Add icons if missing: `copy` exists; add `terminal` (Lucide) only if used.

**Design (follows `docs/ui.md` and house rules F1, F2, F4, B1, V5, C1):**
- New card "API keys" on `/account`, between Passkeys and Sessions. `.card-head` with the count in `.card-sub` ("2 keys") and a top-right `btn-secondary` "New key" (`{{icon "plus"}}`) that opens the shared `sheet` dialog via `hx-get="/account/api-keys/new"`.
- Rows (`.rows.rows-flush` > `.row.row-compact`): `{{icon "key-round"}}`, title = the key's name, sub = `wtfk_<prefix>…` in a `code` span, "Created …", "Last used …" or "Never used". Row action: `btn-sm btn-quiet-danger` "Revoke" opening `{{template "confirm" ...}}` ("Revoke this key?" / "Anything using \"<name>\" will stop working at once." / "Revoke").
- Empty state (`.empty.empty-inset`, `{{icon "key-round"}}`): "No API keys. Create one to use the API or connect Claude Code." with the New key button inside (F1 still has it top-right too: keep one; the empty state's copy points at it, no second button).
- Dialog body `apikey_new.html`, two states: (1) form with one field "Name" (`.field`, `.label`, `.input`, 16px on touch), Create primary, Cancel; error in `.form-error`; (2) after create: "Copy your key now. You won't see it again." the key in a read-only `.input` with a Copy `icon-btn` (`aria-label="Copy key"`, existing `copy` handler in `app.js`), a usage line `curl -H "Authorization: Bearer <key>" <PublicURL>/devices` in a `code` block, and Done (which reloads the card with `hx-get="/account"` `hx-select` the card, or a full reload; pick the pattern the passkey add uses).
- At 10 keys the New key button is hidden (V1) and the card-sub says "10 of 10 keys".

**Routes (page routes, session only, CSRF):** `GET /account/api-keys/new` (fragment), `POST /account/api-keys` (fragment: state 2 or state 1 with error), `POST /account/api-keys/:id/revoke` (303 to `/account`; another user's key: 404 page). Every action audited (`api_key_created`, `api_key_revoked`; never the key).

- [ ] **Step 1: Failing tests (`account_test.go`).** Behaviour: `TestCreateKeyShowsItOnce` (the raw key appears in the create response and not in the next `GET /account`); `TestRevokeAsksFirst` (the row's Revoke opens a confirm dialog, no direct POST link); `TestRevokeAnotherUsersKeyIs404`; `TestNewKeyHiddenAtTheLimit`. Shape: `TestAccountPageHasNoInlineStyle` (existing shape test, extend if present). Security: `TestKeyPagesRefuseAPIKeyAuth` (POST `/account/api-keys` with a bearer key and no cookie: 303 to sign-in).
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Preview fixtures.** Add to `preview_test.go`: account with keys, account with none, key dialog form, form with error, key shown once, revoke confirm. Render, then `shoot.js --only account --viewports phone,desktop --schemes light,dark`; check `report.json` for zero horizontal overflow and zero console errors; look at the phone shots first (P1).
- [ ] **Step 4: Commit.** Stage by name; `git commit -m "feat(web): API keys on the account page"`

---

### Task 10: UI: Connect Claude and Connected apps

**Kind:** UI. **Model:** sonnet. **Depends on:** T5 (grant listing), T8 (routes live), T9 (same files). **Parallel:** none; it runs after T8 and before T11, which reads the final route table.

**Files:**
- Modify: `src/multiuser/web/account.go`, `src/multiuser/web/templates/account.html`, `src/multiuser/web/preview_test.go`, `src/multiuser/web/account_test.go`, `src/multiuser/web/web.go` (the `Deps` struct only); `web.Deps` gains `Grants interface{ ListGrants(ctx, subject) ([]oauth.Grant, error); RevokeGrant(ctx, subject, family string) error }` and `MCPURL string` (set in T8's `Setup`; if T8 has already merged, add them in this task and note it).
- Create: `src/multiuser/web/templates/connect_claude.html`
- Add Lucide icons if missing: `plug`, `external-link`, `terminal`.

**Design:**
- New card "Connected apps" on `/account`, above API keys. Top-right primary button "Connect Claude" (`{{icon "plug"}}`) opening the `sheet` dialog with `connect_claude.html`.
- Dialog: three short blocks, in this order (task words only, C1):
  1. "Claude (web, desktop and mobile)": primary link button "Add to Claude" (`{{icon "external-link"}}`, `target="_blank" rel="noopener"`) to `https://claude.ai/customize/connectors?modal=add-custom-connector&connectorName=<ProductName>&connectorUrl=<MCPURL>` built with `url.Values` (never string formatting). Under it: "Then press Connect and approve."
  2. "Or add it by hand": the MCP URL in a read-only `.input` with a Copy button (the deep link is undocumented; the URL must be visible for manual entry).
  3. "Claude Code": `claude mcp add --transport http <ProductName lower> <MCPURL>` in a code block with Copy.
- Rows: one per grant. `{{icon "plug"}}`, title = client host (`claude.ai`), sub = "Connected …", "Last used …". Action "Disconnect" with confirm ("Disconnect claude.ai?" / "It will lose access at once. You can connect again any time.").
- Empty state: "Nothing connected yet." with the Connect Claude button already in the head.
- `POST /account/connections/:family/revoke` (session, CSRF; 404 for another subject's family). Audit `oauth_grant_revoked`.
- Also the consent page fixtures from T5: consent, consent with loopback warning, consent error.

- [ ] **Step 1: Failing tests.** Behaviour: `TestConnectLinkIsURLEncoded` (an MCP URL containing `?a=b&c=d` round-trips through `url.Parse` of the link's `connectorUrl` exactly); `TestGrantsListOnlyMine`; `TestDisconnectRevokesTheFamily` (the token from that family is refused by `ValidateAccessToken` afterwards); `TestDisconnectAnotherUsersGrantIs404`.
- [ ] **Step 2: Implement, then preview fixtures and screenshots** as in T9 Step 3 (`--only account,consent`).
- [ ] **Step 3: Commit.** `git commit -m "feat(web): connect Claude and manage connected apps"`

---

### Task 11: Cross-user isolation over the new surfaces

**Kind:** backend (security tests). **Model:** opus. **Depends on:** T8, T10. **Parallel:** none.

**Files:**
- Modify: `src/cmd/multiuser_isolation_test.go`

- [ ] **Step 1: Extend `publicRoutes`** with exactly: `GET /.well-known/oauth-authorization-server`, `GET /.well-known/oauth-protected-resource/mcp`, `POST /oauth/token`, `GET /oauth/authorize` (it redirects anonymous browsers to sign-in, so it also passes the 303 rule; list it only if the test's anonymous rule needs it). `POST /mcp` and `DELETE /mcp` are **not** public: anonymous answers 401.
- [ ] **Step 2: Add the two-user matrix** (Alice owns `d_alice`, Bob owns `d_bob`), each case asserting 404/401/tool error and that no usecase fake saw Bob's device:
  - every route the existing test walks, repeated with Alice's API key instead of her cookie;
  - `GET /files/d_bob/<existing file>` with Alice's cookie and with her key;
  - `/mcp` `tools/call` for every tool, with `X-Device-Id: d_bob`, and with `device_id: "d_bob"` in the arguments, using Alice's OAuth token and her API key;
  - Alice disabled: her key, her token and her session all refused on the next request on REST, `/files` and `/mcp`.
- [ ] **Step 3: Break-check one row per surface** (point `files.Mount` at `dm.ResolveDevice`; make `APIKey()` skip the status check; make the MCP context func use `dm.ResolveDevice`). Each must turn the matrix red. Restore.
- [ ] **Step 4: Commit.** `git commit -m "test(wtf): cross-user isolation over API keys, files and MCP"`

---

### Task 12: Docs, Blueprint and the phase 2 go-live

**Kind:** docs + deploy. **Model:** sonnet (the live connect is done by the captain with Aryeh). **Depends on:** T11. **Parallel:** none.

**Files:**
- Modify: `docs/mcp-oauth.md` (a "Multi-user mode (WTF)" section: who the subject is, CIMD only, the host allowlist, no Basic, how to disconnect), `readme.md` (`WTF_MCP_ENABLED`, `WTF_MCP_CLIENT_HOSTS`, API keys in two lines), `render.yaml` (`WTF_MCP_ENABLED: "true"`; nothing else changes: issuer and resource derive from `RENDER_EXTERNAL_URL`), `docs/openapi.yaml` (`file_url` on the download response; `bearerAuth` security scheme naming `wtfk_` keys).

- [ ] **Step 1: Write the docs.** Plain words; no secrets.
- [ ] **Step 2: Verification before completion.** `cd src && go vet ./... && go test ./... 2>&1 | tail -30` (the full suite, here only, by the captain). Then `code-review-loop` on the branch; a separate adversarial review (opus, different agent) over T2 to T8 with the `securing-mcp-connector-oauth` red-flag list.
- [ ] **Step 3: Probe discovery on the deployed preview** with `~/.claude/skills/mcp-oauth-for-claude-connectors/probe-connector-auth.py https://<host>/mcp`. Must pass.
- [ ] **Step 4: Live connect with Aryeh (needs his go-ahead; it connects his real WhatsApp to Claude).** In a real browser: Connect Claude → Add to Claude → Connect → sign in → consent → Approve lands back in Claude (Review Focus 1); ask Claude to list devices and read one recent chat; Disconnect on the Account page and confirm Claude's next call fails. Repeat with Claude Code (`claude mcp add ...`, ephemeral loopback port; Review Focus 2). Then create an API key and `curl` `/devices` with it; revoke it; `curl` again (401).
- [ ] **Step 5: Commit and push the branch** (no tags). Open the PR against `main` with the test evidence and the walk notes.

---

## Deviations from the design doc and defaults chosen (tell the user)

The spec could not be read, so these are measured against the phase 1 plan's description of phase 2.

1. **DCR is off in multi-user mode; CIMD only, from an allowlisted host (`claude.ai`).** The phase 1 plan names CIMD; it does not name DCR. A WhatsApp-access server gains nothing from letting any app register itself. ChatGPT and Grok connectors need DCR or another host; adding `chatgpt.com` to `WTF_MCP_CLIENT_HOSTS` is a one-line change once CIMD support there is confirmed. **Needs Aryeh** only if he wants clients other than Claude.
2. **API keys carry the person's full authority over all their numbers.** No scopes and no per-device keys in this phase; the consent and key dialogs say so in one sentence. Per-device or read-only keys are a later addition if asked for.
3. **API keys are also accepted on `/mcp`**, so Claude Code can use a key instead of OAuth. OAuth stays the path for claude.ai, desktop and mobile.
4. **Owner-checked files are served at `/files/<device_id>/...`, and downloads now land in `statics/media/<device_id>/`** instead of upstream's per-chat-phone folders, so ownership follows the path. Upstream layout is unchanged when multi-user mode is off.
5. **Found while planning, fixed here (Task 2): send-by-URL and per-device webhooks had no SSRF guard.** Harmless while only superadmins could sign in; not once API keys exist.
6. **The `/app/login` REST response still names a QR image under `/statics/qrcode`, which is not served in multi-user mode.** The web UI links devices through its own owner-checked QR route; REST-only linking stays out of scope.
7. **The connector icon will be Render's** while the site lives on `*.onrender.com` (Claude takes the favicon of the host's last two labels). Only a custom domain changes that; cosmetic, costs a domain. Not planned.

## Self-review notes

- Phase 2 scope from the phase 1 plan: personal API keys (T1, T3, T9); MCP per user with OAuth subjects (T5, T7), one-click connect (T10), CIMD (T6); owner-checked file fetching (T4). Added for safety: outbound guard (T2), isolation matrix (T11).
- Type and name checks across tasks: `store.APIKeyPrefix`, `(*Store).UserByAPIKey`, `(*Manager).APIKey`, `netguard.Client`, `files.Mount`/`files.MediaRoot`, `usecase.MediaRootHook`, `utils.OutboundClient`, `oauth.SessionAuthenticator`/`ConsentView`/`CIMDConfig`, `(*oauth.Server).ValidateAccessToken`, `mcpauth.Authenticator`/`Middleware`/`Tools`, `uimcp.Deps.ResolveDevice`/`ExtraTools`, `multiuser.ApplyMCP`, `Settings.MCPResourceURL`.
- Only T1 appends accounts-store migrations; only T8 edits `runtime.go` and `cmd/*.go` wiring; only T9 then T10 edit `account.go`/`account.html`/`preview_test.go`.
