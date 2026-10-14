# WTF Phase 1: Accounts and Ownership Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the GOWA fork into a multi-user server where people sign in over WhatsApp (then passkeys), link their own WhatsApp accounts, and can only ever reach their own accounts, deployed on Render with only superadmins able to sign in.

**Architecture:** A new `src/multiuser/` tree (store, auth, owners, signin, passkey, web) holds all new code. Upstream files get four small, commented hooks: a device-resolver hook in the REST device middleware, an event hook in the WhatsApp event handler, device-scoped message lookups, and a multi-user branch in `cmd/rest.go`. Accounts live in their own SQLite file, `storages/accounts.db`. Upstream databases are untouched.

**Tech Stack:** Go 1.26+ (local toolchain 1.27.2), Fiber v3.4, mattn/go-sqlite3 (CGO; `-tags purego` uses modernc), whatsmeow, `github.com/go-webauthn/webauthn`, `html/template` + `embed`, htmx 2.0.4 (vendored), testify.

**Spec:** the design doc "GOWA multi-user fork: design" at https://claude.ai/code/artifact/24e3a973-7165-4898-a922-a8edb24b9824 (sections: Data model, Sign-up and sign-in, Device ownership and scoping, Web UI, Deployment on Render, Build plan phase 1). Executors must read it.

## Global Constraints

- Module path stays `github.com/aldinokemal/go-whatsapp-web-multidevice` so upstream merges stay clean. New code goes under `src/multiuser/`. Upstream files get only small hooks marked `// WTF:`.
- Run Go commands from `src/`. Baseline: `go test ./...` is green except `usecase TestBuildStickerWebPFFmpegArgsConvertWithInstalledFFmpeg`, which fails only because this Mac's Homebrew ffmpeg has no libwebp. That failure is pre-existing and environmental. Every other test must stay green.
- Product name: **WTF**. It is configurable as `WTF_PRODUCT_NAME` (default `WTF`).
- Multi-user mode is on only when `WTF_MULTIUSER=true`. With it off, the binary behaves exactly like upstream.
- Superadmins come from the env allowlist `WTF_SUPERADMIN_PHONES` (comma-separated E.164 numbers), never from a database column. This deviates from the doc's `is_superadmin` column (see "Deviations from the design doc" at the end).
- Phase 1 sign-up is closed: `WTF_SIGNUP_OPEN` defaults to `false`. Only existing users, and superadmins on first contact, get a sign-in link.
- Sign-in link: single-use, 15 minutes, only a POST consumes it. Session cookie `__Host-wtf_session`, `HttpOnly`, `Secure`, `SameSite=Lax`, 30-day sliding expiry. Only SHA-256 hashes of tokens are stored.
- Never log a token, a session id, `SESSION_SECRET` or message text.
- Browser pages answer with HTML, never JSON. Machine routes (the upstream REST API, `/auth/passkey/*`) answer with JSON.
- UI: `<dialog>` modals only, never `alert()`/`confirm()`. Add buttons sit top right. Actions a user can't take are hidden, not greyed out. Before building templates, load the `frontend-house-style` and `ui-ux-checklist` skills.
- In multi-user mode the following are forced off: MCP (it returns in phase 2), Chatwoot, global webhooks, auto-reply, the gowa-ui dashboard, Basic Auth, the public `/statics` mount, the upstream `/ws` websocket route and auto media download.
- Never run the app against real WhatsApp sessions except during the deploy task, and only with the user's go-ahead.
- Commit after every task. Never push upstream tags to `origin` (they would trigger upstream's release workflows). Push branches only.

## Review Focus

1. **Link previews opening the sign-in link.** WhatsApp and other apps fetch links to build previews. A GET on `/signin/verify` must never consume the token. Pinned in Task 9.
2. **A sender who arrives as a LID with no phone mapping.** No account may be created under a LID. The service number replies that it couldn't read the number, and nothing is stored. Pinned in Task 6.
3. **A user with zero or several phones calling a device API without `device_id`.** They get a clear 400 or 404, never another user's device and never upstream's "default device". Pinned in Task 4.
4. **A disabled user with a live session or passkey.** They are refused on the very next request and the passkey sign-in fails. Pinned in Tasks 3 and 8.
5. **The same WhatsApp number linked by a second user.** The new link is logged out and audited, and the first owner keeps the number. Pinned in Task 4.

---

## File Structure

```
src/multiuser/
  config.go                 Settings struct, Load(), Apply() (forces off unsafe upstream features)
  config_test.go
  store/
    store.go                Open, migrate, Close, ErrNotFound, clock
    tokens.go               NewToken, HashToken, NormalizePhone
    users.go                users table
    logintokens.go          login_tokens table
    sessions.go             sessions table
    owners.go               device_owners table
    settings.go             settings table
    passkeys.go             passkeys table
    audit.go                audit_log table
    store_test.go           (one test file per concern, *_test.go next to each file)
  auth/
    auth.go                 Manager: session middleware, RequirePage, RequireAPI, RequireSuperadmin, CSRF
    context.go              WithUser, UserFrom
    auth_test.go
  owners/
    resolver.go             Resolver.Resolve(ctx, deviceID) (owner-checked device resolution)
    devices.go              ScopedDevices: domainDevice.IDeviceUsecase decorator
    duplicate.go            GuardDuplicatePhone on PairSuccess
    *_test.go
  signin/
    handler.go              service-number message handler: phone -> token -> reply
    ratelimit.go            fixed-window limiter
    *_test.go
  passkey/
    passkey.go              go-webauthn wrapper: register + discoverable login
    passkey_test.go
  web/
    web.go                  Mount(), Deps, render helpers, error pages, CSP
    signin.go               /signin, /signin/verify, /signout, /signin/qr.png
    devices.go              / (My WhatsApp), /ui/devices/* fragments
    account.go              /account, passkey JSON endpoints
    admin.go                /admin/users, /admin/settings
    templates/*.html
    static/{app.css,app.js,htmx.min.js}
    web_test.go
  runtime.go                Setup(): builds store, auth, owners, signin, passkey, web; installs hooks
src/cmd/
  multiuser.go              loadMultiUserEnvConfig(), setupMultiUser()
  admin_link.go             `whatsapp admin-link <phone>`
  multiuser_isolation_test.go  every registered route x two users
render.yaml                 (repo root)
.github/workflows/wtf-ci.yml
```

Upstream files touched (hooks only):
- `src/cmd/root.go`: skip `initApp` for commands annotated `skip-app-init`, and remove the `fmt.Println(viper.AllSettings())` secret leak.
- `src/cmd/rest.go`: a multi-user branch and a top-level `registerDeviceScopedRoutes`.
- `src/ui/rest/middleware/device.go`: the `DeviceResolveFunc` hook.
- `src/infrastructure/whatsapp/event_handler.go`: the `EventHook` var, plus a scoped `DeleteForMe` lookup.
- `src/usecase/message.go` and `src/usecase/send.go`: device-scoped lookups.
- `.github/workflows/{release.yml,build-docker-image.yaml,set-latest-tag.yaml}`: owner gate.

---

### Task 0: Fork hygiene (CI gates, secret leak, CI workflow)

**Files:**
- Modify: `.github/workflows/release.yml`, `.github/workflows/build-docker-image.yaml`, `.github/workflows/set-latest-tag.yaml`
- Create: `.github/workflows/wtf-ci.yml`
- Modify: `src/cmd/root.go:82` (remove `fmt.Println(viper.AllSettings())`)

**Interfaces:** none.

- [ ] **Step 1: Gate upstream workflows to upstream's owner.** Add `if: github.repository_owner == 'aldinokemal'` to **every job** in the three upstream workflow files. Where a job already has an `if:`, AND the two together: `if: github.repository_owner == 'aldinokemal' && (<existing>)`. This keeps the files mergeable while stopping release and Docker publishing from running on the fork.

- [ ] **Step 2: Remove the settings dump.** In `src/cmd/root.go` `initEnvConfig`, delete the line `fmt.Println(viper.AllSettings())`. It prints every env value, including `SESSION_SECRET`, to the Render logs. If `fmt` becomes unused, remove the import.

- [ ] **Step 3: Add the fork's CI.**

```yaml
# .github/workflows/wtf-ci.yml
name: wtf-ci
on:
  push:
    branches: ["**"]
  pull_request:
concurrency:
  group: wtf-ci-${{ github.ref }}
  cancel-in-progress: true
jobs:
  test:
    if: github.repository_owner == 'arilewis93'
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: src
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: src/go.mod
      - run: sudo apt-get update && sudo apt-get install -y ffmpeg webp
      - run: go vet ./...
      - run: set -o pipefail && go test ./... 2>&1 | tee test.log
      - name: The multi-user tests actually ran
        run: grep -E '^ok\s+github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser' test.log
```

- [ ] **Step 4: Verify.** Run `cd src && go build ./... && go vet ./cmd/...`. Expected: no output. The `grep` step will fail until Task 1 creates `multiuser`. That's intended, and Task 1 makes it pass.

- [ ] **Step 5: Commit.**

```bash
git checkout -b wtf/phase-1
git add .github src/cmd/root.go
git commit -m "chore(wtf): gate upstream release workflows, add fork CI, stop logging all settings"
```

---

### Task 1: Configuration and the accounts store foundation

**Files:**
- Create: `src/multiuser/config.go`, `src/multiuser/config_test.go`
- Create: `src/multiuser/store/store.go`, `src/multiuser/store/tokens.go`, `src/multiuser/store/users.go`, `src/multiuser/store/settings.go`, `src/multiuser/store/audit.go`
- Test: `src/multiuser/store/store_test.go`, `src/multiuser/store/users_test.go`

**Interfaces:**
- Produces:
  - `multiuser.Settings{Enabled bool; DBURI, ProductName, PublicURL string; SessionSecret []byte; Superadmins map[string]bool; SignupOpen bool}`
  - `multiuser.Load(getenv func(string) string) (Settings, error)`
  - `multiuser.Apply()` (forces upstream config off)
  - `multiuser.IsSuperadmin(s Settings, phone string) bool`
  - `store.Open(uri string) (*Store, error)`, `(*Store).Close() error`, `(*Store).Now func() time.Time` (a field, overridable in tests)
  - `store.ErrNotFound`
  - `store.NewToken() string` (43-char base64url), `store.HashToken(raw string) string` (hex SHA-256)
  - `store.NormalizePhone(s string) (string, error)`, returning `+<digits>`
  - `store.User{ID, Phone, Status string; MaxDevices int; CreatedAt time.Time; LastLoginAt *time.Time}`, statuses `StatusActive="active"`, `StatusDisabled="disabled"`
  - `(*Store).CreateUser(ctx, phone) (*User, error)`, `UserByPhone(ctx, phone) (*User, error)`, `UserByID(ctx, id) (*User, error)`, `ListUsers(ctx) ([]User, error)`, `SetUserStatus(ctx, id, status) error`, `TouchLogin(ctx, id) error`
  - `(*Store).Setting(ctx, key) (string, bool, error)`, `SetSetting(ctx, key, value, by string) error`, const `SettingServiceDeviceID = "service_device_id"`
  - `(*Store).Audit(ctx, AuditEvent{UserID, Action, DeviceID, IP string}) error`

- [ ] **Step 1: Write the failing store tests.**

```go
// src/multiuser/store/store_test.go
package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open("file:" + filepath.Join(t.TempDir(), "accounts.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestOpenIsIdempotent(t *testing.T) {
	path := "file:" + filepath.Join(t.TempDir(), "accounts.db")
	st, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, st.Close())
	st, err = Open(path) // migrations must be safe to re-run
	require.NoError(t, err)
	require.NoError(t, st.Close())
}

func TestNormalizePhone(t *testing.T) {
	for in, want := range map[string]string{
		"+27 82 555 0100":    "+27825550100",
		"27825550100":        "+27825550100",
		"(+27) 82-555-0100":  "+27825550100",
	} {
		got, err := NormalizePhone(in)
		require.NoError(t, err, in)
		require.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "abc", "+12", "+1234567890123456"} {
		_, err := NormalizePhone(bad)
		require.Error(t, err, bad)
	}
}

func TestTokensAreRandomAndHashed(t *testing.T) {
	a, b := NewToken(), NewToken()
	require.NotEqual(t, a, b)
	require.Len(t, a, 43)
	require.Len(t, HashToken(a), 64)
	require.NotContains(t, HashToken(a), a)
}

func TestSettingsRoundTrip(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	_, ok, err := st.Setting(ctx, SettingServiceDeviceID)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, st.SetSetting(ctx, SettingServiceDeviceID, "d_1", "u1"))
	require.NoError(t, st.SetSetting(ctx, SettingServiceDeviceID, "d_2", "u1"))
	v, ok, err := st.Setting(ctx, SettingServiceDeviceID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "d_2", v)
}

func TestAuditWrites(t *testing.T) {
	st := openTest(t)
	st.Now = func() time.Time { return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) }
	require.NoError(t, st.Audit(context.Background(), AuditEvent{UserID: "u1", Action: "signin", IP: "1.2.3.4"}))
	var n int
	require.NoError(t, st.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='signin'`).Scan(&n))
	require.Equal(t, 1, n)
}
```

```go
// src/multiuser/store/users_test.go
package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateAndFindUser(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "+27825550100")
	require.NoError(t, err)
	require.Equal(t, StatusActive, u.Status)
	require.Equal(t, 2, u.MaxDevices)

	byPhone, err := st.UserByPhone(ctx, "+27825550100")
	require.NoError(t, err)
	require.Equal(t, u.ID, byPhone.ID)

	_, err = st.CreateUser(ctx, "+27825550100")
	require.Error(t, err, "phone is unique")

	_, err = st.UserByPhone(ctx, "+10000000000")
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, st.SetUserStatus(ctx, u.ID, StatusDisabled))
	again, err := st.UserByID(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, again.Status)

	require.Error(t, st.SetUserStatus(ctx, u.ID, "superuser"), "unknown status refused")
}
```

```go
// src/multiuser/config_test.go
package multiuser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadDisabledByDefault(t *testing.T) {
	s, err := Load(env(nil))
	require.NoError(t, err)
	require.False(t, s.Enabled)
}

func TestLoadRequiresStrongSecret(t *testing.T) {
	_, err := Load(env(map[string]string{"WTF_MULTIUSER": "true", "SESSION_SECRET": "short"}))
	require.Error(t, err)
}

func TestLoadPublicURLFallsBackToRender(t *testing.T) {
	s, err := Load(env(map[string]string{
		"WTF_MULTIUSER":         "true",
		"SESSION_SECRET":        "0123456789abcdef0123456789abcdef",
		"RENDER_EXTERNAL_URL":   "https://wtf.onrender.com/",
		"WTF_SUPERADMIN_PHONES": "+27 82 555 0100, 15550001111",
	}))
	require.NoError(t, err)
	require.Equal(t, "https://wtf.onrender.com", s.PublicURL)
	require.Equal(t, "WTF", s.ProductName)
	require.False(t, s.SignupOpen)
	require.True(t, IsSuperadmin(s, "+27825550100"))
	require.True(t, IsSuperadmin(s, "+15550001111"))
	require.False(t, IsSuperadmin(s, "+15550002222"))
}
```

- [ ] **Step 2: Run them and watch them fail.** Run `cd src && go test ./multiuser/...`. Expected: FAIL. The build fails because the package and functions don't exist yet.

- [ ] **Step 3: Implement.**

```go
// src/multiuser/config.go
// Package multiuser is WTF's multi-user layer over GOWA. Everything here is
// inert unless WTF_MULTIUSER=true.
package multiuser

import (
	"errors"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/sirupsen/logrus"
)

type Settings struct {
	Enabled       bool
	DBURI         string
	ProductName   string
	PublicURL     string
	SessionSecret []byte
	Superadmins   map[string]bool
	SignupOpen    bool
}

// Load reads WTF_* settings. getenv is os.Getenv in production and a map in tests.
func Load(getenv func(string) string) (Settings, error) {
	s := Settings{
		Enabled:     strings.EqualFold(strings.TrimSpace(getenv("WTF_MULTIUSER")), "true"),
		DBURI:       "file:storages/accounts.db",
		ProductName: "WTF",
		Superadmins: map[string]bool{},
		SignupOpen:  strings.EqualFold(strings.TrimSpace(getenv("WTF_SIGNUP_OPEN")), "true"),
	}
	if !s.Enabled {
		return s, nil
	}
	if v := strings.TrimSpace(getenv("WTF_DB_URI")); v != "" {
		s.DBURI = v
	}
	if v := strings.TrimSpace(getenv("WTF_PRODUCT_NAME")); v != "" {
		s.ProductName = v
	}
	s.PublicURL = strings.TrimSpace(getenv("WTF_PUBLIC_URL"))
	if s.PublicURL == "" {
		s.PublicURL = strings.TrimSpace(getenv("RENDER_EXTERNAL_URL"))
	}
	if s.PublicURL == "" {
		s.PublicURL = "http://localhost:3000"
	}
	s.PublicURL = strings.TrimRight(s.PublicURL, "/")

	secret := getenv("SESSION_SECRET")
	if len(secret) < 32 {
		return s, errors.New("SESSION_SECRET must be at least 32 characters when WTF_MULTIUSER=true")
	}
	s.SessionSecret = []byte(secret)

	for _, raw := range strings.Split(getenv("WTF_SUPERADMIN_PHONES"), ",") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		phone, err := store.NormalizePhone(raw)
		if err != nil {
			return s, errors.New("WTF_SUPERADMIN_PHONES holds an invalid number")
		}
		s.Superadmins[phone] = true
	}
	return s, nil
}

func IsSuperadmin(s Settings, phone string) bool { return s.Superadmins[phone] }

// Apply turns off every upstream feature that is unsafe when several people
// share one server. Phase 2 re-enables MCP with per-user OAuth.
func Apply() {
	if len(config.AppBasicAuthCredential) > 0 {
		logrus.Warn("[WTF] APP_BASIC_AUTH is ignored in multi-user mode")
	}
	config.AppBasicAuthCredential = nil
	config.McpEnabled = false
	config.McpOAuthEnabled = false
	config.ChatwootEnabled = false
	config.WhatsappWebhook = nil
	config.WhatsappAutoReplyMessage = ""
	config.AppUIEnabled = false
	config.WhatsappAutoDownloadMedia = false
}
```

Add one more test for `Apply` to `config_test.go`:

```go
func TestApplyForcesSharedFeaturesOff(t *testing.T) {
	saved := []any{config.McpEnabled, config.ChatwootEnabled, config.WhatsappWebhook, config.AppUIEnabled, config.AppBasicAuthCredential}
	t.Cleanup(func() {
		config.McpEnabled = saved[0].(bool)
		config.ChatwootEnabled = saved[1].(bool)
		config.WhatsappWebhook = saved[2].([]string)
		config.AppUIEnabled = saved[3].(bool)
		config.AppBasicAuthCredential = saved[4].([]string)
	})
	config.McpEnabled, config.ChatwootEnabled, config.AppUIEnabled = true, true, true
	config.WhatsappWebhook = []string{"https://hook"}
	config.AppBasicAuthCredential = []string{"a:b"}
	Apply()
	require.False(t, config.McpEnabled)
	require.False(t, config.ChatwootEnabled)
	require.False(t, config.AppUIEnabled)
	require.Empty(t, config.WhatsappWebhook)
	require.Empty(t, config.AppBasicAuthCredential)
}
```

The test file also needs the import `"github.com/aldinokemal/go-whatsapp-web-multidevice/config"`.

```go
// src/multiuser/store/store.go
// Package store holds WTF's accounts database (storages/accounts.db).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	db  *sql.DB
	Now func() time.Time
}

func Open(uri string) (*Store, error) {
	db, err := sql.Open(sqlite.DriverName, sqlite.FormatChatStorageURI(uri, true, true))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("accounts db: %w", err)
	}
	st := &Store{db: db, Now: func() time.Time { return time.Now().UTC() }}
	if err := st.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("accounts db migrate: %w", err)
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrations run in order, once each. Append only; never edit a shipped entry.
var migrations = []string{
	`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		phone TEXT NOT NULL UNIQUE,
		status TEXT NOT NULL DEFAULT 'active',
		max_devices INTEGER NOT NULL DEFAULT 2,
		created_at TIMESTAMP NOT NULL,
		last_login_at TIMESTAMP NULL)`,
	`CREATE TABLE login_tokens (
		token_hash TEXT PRIMARY KEY,
		phone TEXT NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		used_at TIMESTAMP NULL)`,
	`CREATE TABLE sessions (
		id_hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TIMESTAMP NOT NULL,
		expires_at TIMESTAMP NOT NULL,
		user_agent TEXT NOT NULL DEFAULT '')`,
	`CREATE INDEX sessions_user ON sessions(user_id)`,
	`CREATE TABLE passkeys (
		credential_id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		credential_json TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL,
		last_used_at TIMESTAMP NULL)`,
	`CREATE INDEX passkeys_user ON passkeys(user_id)`,
	`CREATE TABLE device_owners (
		device_id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		created_at TIMESTAMP NOT NULL)`,
	`CREATE INDEX device_owners_user ON device_owners(user_id)`,
	`CREATE TABLE settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_by TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMP NOT NULL)`,
	`CREATE TABLE audit_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id TEXT NOT NULL DEFAULT '',
		action TEXT NOT NULL,
		device_id TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL DEFAULT '',
		at TIMESTAMP NOT NULL)`,
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS wtf_schema (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var version int
	err := s.db.QueryRowContext(ctx, `SELECT version FROM wtf_schema`).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO wtf_schema(version) VALUES (0)`); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE wtf_schema SET version = ?`, i+1); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
```

```go
// src/multiuser/store/tokens.go
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// NewToken returns 32 random bytes, base64url without padding (43 characters).
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is what we store: hex SHA-256 of the raw token.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// NormalizePhone turns any human spelling into E.164 "+<digits>" (8 to 15 digits).
func NormalizePhone(s string) (string, error) {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if len(digits) < 8 || len(digits) > 15 {
		return "", errors.New("not a phone number")
	}
	return "+" + digits, nil
}

func newID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return prefix + hex.EncodeToString(b)
}
```

```go
// src/multiuser/store/users.go
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

type User struct {
	ID          string
	Phone       string
	Status      string
	MaxDevices  int
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

const userCols = `id, phone, status, max_devices, created_at, last_login_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var last sql.NullTime
	if err := row.Scan(&u.ID, &u.Phone, &u.Status, &u.MaxDevices, &u.CreatedAt, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if last.Valid {
		t := last.Time
		u.LastLoginAt = &t
	}
	return &u, nil
}

func (s *Store) CreateUser(ctx context.Context, phone string) (*User, error) {
	u := &User{ID: newID("u_"), Phone: phone, Status: StatusActive, MaxDevices: 2, CreatedAt: s.Now()}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, phone, status, max_devices, created_at) VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Phone, u.Status, u.MaxDevices, u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

func (s *Store) UserByPhone(ctx context.Context, phone string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE phone = ?`, phone))
}

func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

// ListUsers returns every user, newest first. Phase 1 has few users; phase 4
// adds paging when sign-up opens.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) SetUserStatus(ctx context.Context, id, status string) error {
	if status != StatusActive && status != StatusDisabled {
		return fmt.Errorf("unknown status %q", status)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE users SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) TouchLogin(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, s.Now(), id)
	return err
}
```

```go
// src/multiuser/store/settings.go
package store

import (
	"context"
	"database/sql"
	"errors"
)

const SettingServiceDeviceID = "service_device_id"

func (s *Store) Setting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(ctx context.Context, key, value, by string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_by, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		key, value, by, s.Now())
	return err
}
```

```go
// src/multiuser/store/audit.go
package store

import "context"

type AuditEvent struct {
	UserID   string
	Action   string
	DeviceID string
	IP       string
}

func (s *Store) Audit(ctx context.Context, e AuditEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (user_id, action, device_id, ip, at) VALUES (?, ?, ?, ?, ?)`,
		e.UserID, e.Action, e.DeviceID, e.IP, s.Now())
	return err
}
```

- [ ] **Step 4: Run the tests.** Run `cd src && go test ./multiuser/... -v`. Expected: PASS. Also run `go test -tags purego ./multiuser/...`. Expected: PASS (proves the modernc build).

- [ ] **Step 5: Break-it check.** Temporarily change `len(digits) < 8` to `< 2` and confirm `TestNormalizePhone` goes red. Then revert.

- [ ] **Step 6: Commit.**

```bash
git add src/multiuser
git commit -m "feat(wtf): multi-user settings and accounts store foundation"
```

---

### Task 2: Login tokens, sessions, device owners and passkey rows

**Files:**
- Create: `src/multiuser/store/logintokens.go`, `sessions.go`, `owners.go`, `passkeys.go`
- Test: `src/multiuser/store/logintokens_test.go`, `sessions_test.go`, `owners_test.go`, `passkeys_test.go`

**Interfaces:**
- Consumes: `Store`, `User`, `NewToken`, `HashToken`, `ErrNotFound` (Task 1).
- Produces:
  - `(*Store).CreateLoginToken(ctx, phone string, ttl time.Duration) (raw string, err error)`
  - `(*Store).LoginTokenValid(ctx, raw string) (bool, error)`: read-only, for the GET page
  - `(*Store).ConsumeLoginToken(ctx, raw string) (phone string, err error)`: atomic and single-use; `ErrNotFound` when used, expired or unknown
  - `store.Session{UserID string; ExpiresAt time.Time}`
  - `(*Store).CreateSession(ctx, userID, userAgent string, ttl time.Duration) (raw string, err error)`
  - `(*Store).SessionUser(ctx, raw string, ttl time.Duration) (*User, error)`: `ErrNotFound` if missing or expired. It slides the expiry when less than half the TTL is left. It returns disabled users too; callers check `Status`.
  - `(*Store).DeleteSession(ctx, raw) error`, `DeleteUserSessions(ctx, userID) error`
  - `(*Store).SetOwner(ctx, deviceID, userID string) error`, `OwnerOf(ctx, deviceID) (string, error)` (`ErrNotFound`), `DevicesOf(ctx, userID) ([]string, error)` (sorted by `created_at`), `RemoveOwner(ctx, deviceID) error`, `OwnedDeviceCount(ctx) (int, error)`
  - `store.Passkey{CredentialID, UserID, Name string; CredentialJSON []byte; CreatedAt time.Time; LastUsedAt *time.Time}`
  - `(*Store).AddPasskey(ctx, Passkey) error`, `PasskeysOf(ctx, userID) ([]Passkey, error)`, `PasskeyByCredentialID(ctx, credID) (*Passkey, error)`, `UpdatePasskeyCredential(ctx, credID string, credJSON []byte) error` (also sets `last_used_at`), `DeletePasskey(ctx, userID, credID string) error` (`ErrNotFound` if it isn't that user's)

- [ ] **Step 1: Write the failing tests.**

```go
// src/multiuser/store/logintokens_test.go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoginTokenSingleUseAndExpiry(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }

	raw, err := st.CreateLoginToken(ctx, "+27825550100", 15*time.Minute)
	require.NoError(t, err)

	var stored string
	require.NoError(t, st.db.QueryRow(`SELECT token_hash FROM login_tokens`).Scan(&stored))
	require.NotEqual(t, raw, stored, "only the hash is stored")

	ok, err := st.LoginTokenValid(ctx, raw)
	require.NoError(t, err)
	require.True(t, ok)
	ok, _ = st.LoginTokenValid(ctx, raw) // peeking twice never consumes
	require.True(t, ok)

	phone, err := st.ConsumeLoginToken(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, "+27825550100", phone)

	_, err = st.ConsumeLoginToken(ctx, raw)
	require.ErrorIs(t, err, ErrNotFound, "second use refused")

	late, _ := st.CreateLoginToken(ctx, "+27825550100", 15*time.Minute)
	now = now.Add(16 * time.Minute)
	ok, _ = st.LoginTokenValid(ctx, late)
	require.False(t, ok)
	_, err = st.ConsumeLoginToken(ctx, late)
	require.ErrorIs(t, err, ErrNotFound, "expired refused")

	_, err = st.ConsumeLoginToken(ctx, "nonsense")
	require.ErrorIs(t, err, ErrNotFound)
}
```

```go
// src/multiuser/store/sessions_test.go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionLifecycle(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }
	u, _ := st.CreateUser(ctx, "+27825550100")
	ttl := 30 * 24 * time.Hour

	raw, err := st.CreateSession(ctx, u.ID, "Safari on iOS", ttl)
	require.NoError(t, err)

	got, err := st.SessionUser(ctx, raw, ttl)
	require.NoError(t, err)
	require.Equal(t, u.ID, got.ID)

	// 20 days on: less than half left, so the expiry slides forward.
	now = now.Add(20 * 24 * time.Hour)
	_, err = st.SessionUser(ctx, raw, ttl)
	require.NoError(t, err)
	now = now.Add(20 * 24 * time.Hour) // 40 days after creation, still alive thanks to the slide
	_, err = st.SessionUser(ctx, raw, ttl)
	require.NoError(t, err)

	now = now.Add(31 * 24 * time.Hour)
	_, err = st.SessionUser(ctx, raw, ttl)
	require.ErrorIs(t, err, ErrNotFound, "expired")

	raw2, _ := st.CreateSession(ctx, u.ID, "", ttl)
	raw3, _ := st.CreateSession(ctx, u.ID, "", ttl)
	require.NoError(t, st.DeleteSession(ctx, raw2))
	_, err = st.SessionUser(ctx, raw2, ttl)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, st.DeleteUserSessions(ctx, u.ID))
	_, err = st.SessionUser(ctx, raw3, ttl)
	require.ErrorIs(t, err, ErrNotFound)
}
```

```go
// src/multiuser/store/owners_test.go
package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnership(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550101")

	require.NoError(t, st.SetOwner(ctx, "d_a1", a.ID))
	require.NoError(t, st.SetOwner(ctx, "d_b1", b.ID))
	require.Error(t, st.SetOwner(ctx, "d_a1", b.ID), "a device has one owner; no silent takeover")

	owner, err := st.OwnerOf(ctx, "d_a1")
	require.NoError(t, err)
	require.Equal(t, a.ID, owner)
	_, err = st.OwnerOf(ctx, "d_nobody")
	require.ErrorIs(t, err, ErrNotFound)

	ids, err := st.DevicesOf(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"d_a1"}, ids)

	n, _ := st.OwnedDeviceCount(ctx)
	require.Equal(t, 2, n)
	require.NoError(t, st.RemoveOwner(ctx, "d_a1"))
	ids, _ = st.DevicesOf(ctx, a.ID)
	require.Empty(t, ids)
}
```

```go
// src/multiuser/store/passkeys_test.go
package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPasskeys(t *testing.T) {
	st := openTest(t)
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550101")
	require.NoError(t, st.AddPasskey(ctx, Passkey{CredentialID: "cred1", UserID: a.ID, Name: "Safari on iOS", CredentialJSON: []byte(`{}`)}))

	list, err := st.PasskeysOf(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)

	pk, err := st.PasskeyByCredentialID(ctx, "cred1")
	require.NoError(t, err)
	require.Equal(t, a.ID, pk.UserID)

	require.NoError(t, st.UpdatePasskeyCredential(ctx, "cred1", []byte(`{"n":1}`)))
	pk, _ = st.PasskeyByCredentialID(ctx, "cred1")
	require.JSONEq(t, `{"n":1}`, string(pk.CredentialJSON))
	require.NotNil(t, pk.LastUsedAt)

	require.ErrorIs(t, st.DeletePasskey(ctx, b.ID, "cred1"), ErrNotFound, "cannot delete another user's passkey")
	require.NoError(t, st.DeletePasskey(ctx, a.ID, "cred1"))
}
```

- [ ] **Step 2: Run them and watch them fail.** Run `cd src && go test ./multiuser/store/`. Expected: FAIL (undefined methods).

- [ ] **Step 3: Implement.**

```go
// src/multiuser/store/logintokens.go
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) CreateLoginToken(ctx context.Context, phone string, ttl time.Duration) (string, error) {
	raw := NewToken()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO login_tokens (token_hash, phone, expires_at) VALUES (?, ?, ?)`,
		HashToken(raw), phone, s.Now().Add(ttl))
	if err != nil {
		return "", err
	}
	// Opportunistic cleanup of dead tokens.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM login_tokens WHERE expires_at < ?`, s.Now().Add(-24*time.Hour))
	return raw, nil
}

func (s *Store) LoginTokenValid(ctx context.Context, raw string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM login_tokens WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		HashToken(raw), s.Now()).Scan(&n)
	return n == 1, err
}

// ConsumeLoginToken marks the token used in the same statement that checks it,
// so two concurrent POSTs cannot both succeed.
func (s *Store) ConsumeLoginToken(ctx context.Context, raw string) (string, error) {
	h := HashToken(raw)
	res, err := s.db.ExecContext(ctx,
		`UPDATE login_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		s.Now(), h, s.Now())
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return "", ErrNotFound
	}
	var phone string
	if err := s.db.QueryRowContext(ctx, `SELECT phone FROM login_tokens WHERE token_hash = ?`, h).Scan(&phone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return phone, nil
}
```

```go
// src/multiuser/store/sessions.go
package store

import (
	"context"
	"time"
)

func (s *Store) CreateSession(ctx context.Context, userID, userAgent string, ttl time.Duration) (string, error) {
	raw := NewToken()
	now := s.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id_hash, user_id, created_at, expires_at, user_agent) VALUES (?, ?, ?, ?, ?)`,
		HashToken(raw), userID, now, now.Add(ttl), userAgent)
	if err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Store) SessionUser(ctx context.Context, raw string, ttl time.Duration) (*User, error) {
	h := HashToken(raw)
	var expires time.Time
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id, expires_at FROM sessions WHERE id_hash = ?`, h).Scan(&userID, &expires)
	if err != nil {
		return nil, ErrNotFound
	}
	now := s.Now()
	if !expires.After(now) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, h)
		return nil, ErrNotFound
	}
	if expires.Sub(now) < ttl/2 {
		_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET expires_at = ? WHERE id_hash = ?`, now.Add(ttl), h)
	}
	return s.UserByID(ctx, userID)
}

func (s *Store) DeleteSession(ctx context.Context, raw string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id_hash = ?`, HashToken(raw))
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}
```

```go
// src/multiuser/store/owners.go
package store

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) SetOwner(ctx context.Context, deviceID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO device_owners (device_id, user_id, created_at) VALUES (?, ?, ?)`, deviceID, userID, s.Now())
	return err
}

func (s *Store) OwnerOf(ctx context.Context, deviceID string) (string, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM device_owners WHERE device_id = ?`, deviceID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return userID, err
}

func (s *Store) DevicesOf(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id FROM device_owners WHERE user_id = ? ORDER BY created_at, device_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) RemoveOwner(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM device_owners WHERE device_id = ?`, deviceID)
	return err
}

func (s *Store) OwnedDeviceCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_owners`).Scan(&n)
	return n, err
}
```

```go
// src/multiuser/store/passkeys.go
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Passkey struct {
	CredentialID   string // base64url of the WebAuthn credential id
	UserID         string
	Name           string
	CredentialJSON []byte // json of webauthn.Credential
	CreatedAt      time.Time
	LastUsedAt     *time.Time
}

const passkeyCols = `credential_id, user_id, name, credential_json, created_at, last_used_at`

func scanPasskey(row interface{ Scan(...any) error }) (*Passkey, error) {
	var p Passkey
	var js string
	var last sql.NullTime
	if err := row.Scan(&p.CredentialID, &p.UserID, &p.Name, &js, &p.CreatedAt, &last); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.CredentialJSON = []byte(js)
	if last.Valid {
		t := last.Time
		p.LastUsedAt = &t
	}
	return &p, nil
}

func (s *Store) AddPasskey(ctx context.Context, p Passkey) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO passkeys (credential_id, user_id, name, credential_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		p.CredentialID, p.UserID, p.Name, string(p.CredentialJSON), s.Now())
	return err
}

func (s *Store) PasskeysOf(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+passkeyCols+` FROM passkeys WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		p, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) PasskeyByCredentialID(ctx context.Context, credID string) (*Passkey, error) {
	return scanPasskey(s.db.QueryRowContext(ctx, `SELECT `+passkeyCols+` FROM passkeys WHERE credential_id = ?`, credID))
}

func (s *Store) UpdatePasskeyCredential(ctx context.Context, credID string, credJSON []byte) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE passkeys SET credential_json = ?, last_used_at = ? WHERE credential_id = ?`, string(credJSON), s.Now(), credID)
	return err
}

func (s *Store) DeletePasskey(ctx context.Context, userID, credID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE credential_id = ? AND user_id = ?`, credID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run the tests.** Run `cd src && go test ./multiuser/store/ -v && go test -tags purego ./multiuser/store/`. Expected: PASS.

- [ ] **Step 5: Break-it check.** Remove `AND used_at IS NULL` from `ConsumeLoginToken` and confirm `TestLoginTokenSingleUseAndExpiry` fails. Then revert.

- [ ] **Step 6: Commit.**

```bash
git add src/multiuser/store
git commit -m "feat(wtf): login tokens, sessions, device ownership and passkey storage"
```

---

### Task 3: Auth manager (sessions, page and API gates, superadmin, CSRF)

**Files:**
- Create: `src/multiuser/auth/context.go`, `src/multiuser/auth/auth.go`
- Test: `src/multiuser/auth/auth_test.go`

**Interfaces:**
- Consumes: `store.Store`, `store.User`, `store.StatusActive` (Tasks 1 and 2), and `multiuser.Settings` shape (passed as fields to avoid an import cycle).
- Produces:
  - `auth.WithUser(ctx, *store.User) context.Context`, `auth.UserFrom(ctx) (*store.User, bool)`
  - `auth.Config{Secret []byte; Superadmins map[string]bool; SessionTTL time.Duration; Secure bool}`
  - `auth.CookieName = "__Host-wtf_session"`; when `Secure` is false (plain-http local dev) the cookie name is `wtf_session`, because browsers refuse `__Host-` without `Secure`
  - `auth.New(cfg Config, st *store.Store) *Manager`
  - `(*Manager).Middleware() fiber.Handler`: always mounted; loads the user when the cookie is valid and the user is active, then sets `c.SetContext(WithUser(...))` and `c.Locals("wtf_user")`
  - `(*Manager).RequirePage() fiber.Handler`: no user means a 303 to `/signin?next=<path>`
  - `(*Manager).RequireAPI() fiber.Handler`: no user means a 401 JSON `{"status":401,"code":"UNAUTHENTICATED","message":"sign in required"}`
  - `(*Manager).RequireSuperadmin() fiber.Handler`: 404 page for non-superadmins (don't reveal admin pages)
  - `(*Manager).IsSuperadmin(u *store.User) bool`
  - `(*Manager).StartSession(c fiber.Ctx, u *store.User) error`, `(*Manager).EndSession(c fiber.Ctx) error`
  - `(*Manager).CSRFToken(c fiber.Ctx) string`
  - `(*Manager).VerifyCSRF() fiber.Handler`: for POST/PUT/PATCH/DELETE with a session, requires form field `csrf` or header `X-CSRF-Token`; 403 otherwise. Requests without a session pass through (the gates refuse them).

- [ ] **Step 1: Write the failing tests.**

```go
// src/multiuser/auth/auth_test.go
package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) (*fiber.App, *Manager, *store.Store) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	m := New(Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: 30 * 24 * time.Hour,
		Superadmins: map[string]bool{"+27825550100": true}, Secure: true}, st)
	app := fiber.New()
	app.Use(m.Middleware())
	app.Get("/page", m.RequirePage(), func(c fiber.Ctx) error {
		u, _ := UserFrom(c.Context())
		return c.SendString("hello " + u.Phone)
	})
	app.Post("/act", m.RequirePage(), m.VerifyCSRF(), func(c fiber.Ctx) error { return c.SendString("done") })
	app.Get("/api/thing", m.RequireAPI(), func(c fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/admin", m.RequirePage(), m.RequireSuperadmin(), func(c fiber.Ctx) error { return c.SendString("admin") })
	app.Get("/login-as/:id", func(c fiber.Ctx) error {
		u, err := st.UserByID(c.Context(), c.Params("id"))
		require.NoError(t, err)
		return m.StartSession(c, u)
	})
	app.Get("/csrf", m.RequirePage(), func(c fiber.Ctx) error { return c.SendString(m.CSRFToken(c)) })
	return app, m, st
}

func login(t *testing.T, app *fiber.App, userID string) *http.Cookie {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/login-as/"+userID, nil))
	require.NoError(t, err)
	for _, c := range resp.Cookies() {
		if c.Name == CookieName {
			require.True(t, c.HttpOnly)
			require.True(t, c.Secure)
			require.Equal(t, http.SameSiteLaxMode, c.SameSite)
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

func get(t *testing.T, app *fiber.App, path string, ck *http.Cookie) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if ck != nil {
		req.AddCookie(ck)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func TestAnonymousPageRedirectsAndAPIGets401(t *testing.T) {
	app, _, _ := setup(t)
	resp := get(t, app, "/page", nil)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/signin?next=%2Fpage", resp.Header.Get("Location"))

	resp = get(t, app, "/api/thing", nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

func TestSessionGrantsAccessAndDisabledUserIsRefusedAtOnce(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)

	resp := get(t, app, "/page", ck)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "hello +27825550199", string(body))

	require.NoError(t, st.SetUserStatus(context.Background(), u.ID, store.StatusDisabled))
	resp = get(t, app, "/page", ck)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "disabled user is signed out on the next request")
	resp = get(t, app, "/api/thing", ck)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSuperadminComesFromAllowlistOnly(t *testing.T) {
	app, _, st := setup(t)
	admin, _ := st.CreateUser(context.Background(), "+27825550100")
	plain, _ := st.CreateUser(context.Background(), "+27825550101")
	require.Equal(t, http.StatusOK, get(t, app, "/admin", login(t, app, admin.ID)).StatusCode)
	require.Equal(t, http.StatusNotFound, get(t, app, "/admin", login(t, app, plain.ID)).StatusCode)
}

func TestCSRFRequiredOnUnsafeMethods(t *testing.T) {
	app, _, st := setup(t)
	u, _ := st.CreateUser(context.Background(), "+27825550199")
	ck := login(t, app, u.ID)

	post := func(form url.Values, header string) int {
		req := httptest.NewRequest(http.MethodPost, "/act", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if header != "" {
			req.Header.Set("X-CSRF-Token", header)
		}
		req.AddCookie(ck)
		resp, err := app.Test(req)
		require.NoError(t, err)
		return resp.StatusCode
	}
	require.Equal(t, http.StatusForbidden, post(url.Values{}, ""))
	require.Equal(t, http.StatusForbidden, post(url.Values{"csrf": {"wrong"}}, ""))

	tokResp := get(t, app, "/csrf", ck)
	tok, _ := io.ReadAll(tokResp.Body)
	require.Equal(t, http.StatusOK, post(url.Values{"csrf": {string(tok)}}, ""))
	require.Equal(t, http.StatusOK, post(url.Values{}, string(tok)))
}
```

- [ ] **Step 2: Run them and watch them fail.** Run `cd src && go test ./multiuser/auth/`. Expected: FAIL (package missing).

- [ ] **Step 3: Implement.**

```go
// src/multiuser/auth/context.go
package auth

import (
	"context"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
)

type ctxKey struct{}

func WithUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

func UserFrom(ctx context.Context) (*store.User, bool) {
	u, ok := ctx.Value(ctxKey{}).(*store.User)
	return u, ok && u != nil
}
```

```go
// src/multiuser/auth/auth.go
// Package auth is WTF's browser session layer: cookie sessions, the page/API
// gates, the superadmin allowlist and CSRF.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/url"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
)

var CookieName = "__Host-wtf_session"

const localsRaw = "wtf_session_raw"

type Config struct {
	Secret      []byte
	Superadmins map[string]bool
	SessionTTL  time.Duration
	Secure      bool // false only for plain-http local development
}

type Manager struct {
	cfg Config
	st  *store.Store
}

func New(cfg Config, st *store.Store) *Manager {
	if !cfg.Secure {
		CookieName = "wtf_session"
	}
	return &Manager{cfg: cfg, st: st}
}

func (m *Manager) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		raw := c.Cookies(CookieName)
		if raw == "" {
			return c.Next()
		}
		u, err := m.st.SessionUser(c.Context(), raw, m.cfg.SessionTTL)
		if err != nil || u.Status != store.StatusActive {
			m.clearCookie(c)
			return c.Next()
		}
		c.Locals(localsRaw, raw)
		c.Locals("wtf_user", u)
		c.SetContext(WithUser(c.Context(), u))
		return c.Next()
	}
}

func (m *Manager) RequirePage() fiber.Handler {
	return func(c fiber.Ctx) error {
		if _, ok := UserFrom(c.Context()); ok {
			return c.Next()
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/signin?next=" + url.QueryEscape(c.OriginalURL()))
	}
}

func (m *Manager) RequireAPI() fiber.Handler {
	return func(c fiber.Ctx) error {
		if _, ok := UserFrom(c.Context()); ok {
			return c.Next()
		}
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"status": fiber.StatusUnauthorized, "code": "UNAUTHENTICATED", "message": "sign in required",
		})
	}
}

func (m *Manager) IsSuperadmin(u *store.User) bool { return u != nil && m.cfg.Superadmins[u.Phone] }

func (m *Manager) RequireSuperadmin() fiber.Handler {
	return func(c fiber.Ctx) error {
		u, _ := UserFrom(c.Context())
		if !m.IsSuperadmin(u) {
			return fiber.ErrNotFound
		}
		return c.Next()
	}
}

func (m *Manager) StartSession(c fiber.Ctx, u *store.User) error {
	raw, err := m.st.CreateSession(c.Context(), u.ID, c.Get(fiber.HeaderUserAgent), m.cfg.SessionTTL)
	if err != nil {
		return err
	}
	_ = m.st.TouchLogin(c.Context(), u.ID)
	c.Cookie(&fiber.Cookie{
		Name: CookieName, Value: raw, Path: "/", HTTPOnly: true, Secure: m.cfg.Secure,
		SameSite: fiber.CookieSameSiteLaxMode, MaxAge: int(m.cfg.SessionTTL.Seconds()),
	})
	c.Locals(localsRaw, raw)
	c.SetContext(WithUser(c.Context(), u))
	return nil
}

func (m *Manager) EndSession(c fiber.Ctx) error {
	if raw, ok := c.Locals(localsRaw).(string); ok && raw != "" {
		if err := m.st.DeleteSession(c.Context(), raw); err != nil {
			return err
		}
	}
	m.clearCookie(c)
	return nil
}

func (m *Manager) clearCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{Name: CookieName, Value: "", Path: "/", HTTPOnly: true, Secure: m.cfg.Secure,
		SameSite: fiber.CookieSameSiteLaxMode, MaxAge: -1})
}

// CSRFToken is HMAC(secret, session) so it needs no storage and dies with the session.
func (m *Manager) CSRFToken(c fiber.Ctx) string {
	raw, _ := c.Locals(localsRaw).(string)
	if raw == "" {
		return ""
	}
	mac := hmac.New(sha256.New, m.cfg.Secret)
	mac.Write([]byte("csrf:" + raw))
	return hex.EncodeToString(mac.Sum(nil))
}

func (m *Manager) VerifyCSRF() fiber.Handler {
	return func(c fiber.Ctx) error {
		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
			return c.Next()
		}
		want := m.CSRFToken(c)
		if want == "" { // no session: the gates decide
			return c.Next()
		}
		got := c.Get("X-CSRF-Token")
		if got == "" {
			got = c.FormValue("csrf")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			return fiber.NewError(fiber.StatusForbidden, "This form has expired. Reload the page and try again.")
		}
		return c.Next()
	}
}
```

Note: `RequireSuperadmin` returns `fiber.ErrNotFound`. The web package's error handler (Task 9) renders it as a friendly 404 page. In this task's test, Fiber's default handler returns 404, which is what the test asserts.

- [ ] **Step 4: Run the tests.** Run `cd src && go test ./multiuser/auth/ -v`. Expected: PASS.

- [ ] **Step 5: Break-it check.** In `Middleware`, delete `|| u.Status != store.StatusActive` and confirm `TestSessionGrantsAccessAndDisabledUserIsRefusedAtOnce` fails. Then revert.

- [ ] **Step 6: Commit.**

```bash
git add src/multiuser/auth
git commit -m "feat(wtf): session auth, page/API gates, superadmin allowlist, CSRF"
```

---

### Task 4: Ownership (resolver, scoped device usecase, duplicate-phone guard)

**Files:**
- Create: `src/multiuser/owners/resolver.go`, `src/multiuser/owners/devices.go`, `src/multiuser/owners/duplicate.go`
- Test: `src/multiuser/owners/resolver_test.go`, `src/multiuser/owners/devices_test.go`, `src/multiuser/owners/duplicate_test.go`

**Interfaces:**
- Consumes: `auth.UserFrom` (Task 3); `store.SetOwner/OwnerOf/DevicesOf/RemoveOwner/Audit` (Task 2); upstream `whatsapp.DeviceManager.GetDevice(id) (*DeviceInstance, bool)`, `domainDevice.IDeviceUsecase`, `pkgError.ErrDeviceNotFound`, `pkgError.ValidationError`.
- Produces:
  - `owners.DeviceGetter interface{ GetDevice(id string) (*whatsapp.DeviceInstance, bool) }`
  - `owners.NewResolver(dm DeviceGetter, st *store.Store) *Resolver`
  - `(*Resolver).Resolve(ctx context.Context, deviceID string) (*whatsapp.DeviceInstance, string, error)`: same contract as upstream `DeviceManager.ResolveDevice` (on "provided but not found" it returns the id with an error), so the upstream middleware's 404/400 mapping keeps working
  - `owners.ErrDeviceIDRequired = pkgError.ValidationError("you have several WhatsApp accounts linked; pass device_id or the X-Device-Id header")`
  - `owners.ErrNoDeviceLinked = pkgError.ValidationError("link a WhatsApp account first")`
  - `owners.NewScopedDevices(inner domainDevice.IDeviceUsecase, st *store.Store) domainDevice.IDeviceUsecase`
  - `owners.PairedDevice{ID, JID string}`, `owners.PairSource interface{ Paired() []PairedDevice; Logout(ctx context.Context, deviceID string) error }`
  - `owners.GuardDuplicatePhone(ctx, src PairSource, st *store.Store, deviceID string) (refused bool, err error)`
  - `owners.NewDeviceID() string` returning `d_` + 32 hex characters

- [ ] **Step 1: Write the failing tests.**

```go
// src/multiuser/owners/resolver_test.go
package owners

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

type fakeDM map[string]*whatsapp.DeviceInstance

func (f fakeDM) GetDevice(id string) (*whatsapp.DeviceInstance, bool) { d, ok := f[id]; return d, ok }

func fixture(t *testing.T) (*store.Store, fakeDM, *store.User, *store.User) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	a, _ := st.CreateUser(ctx, "+27825550100")
	b, _ := st.CreateUser(ctx, "+27825550101")
	dm := fakeDM{}
	for _, id := range []string{"d_a1", "d_a2", "d_b1", "d_orphan"} {
		dm[id] = whatsapp.NewDeviceInstance(id, nil, nil)
	}
	require.NoError(t, st.SetOwner(ctx, "d_a1", a.ID))
	require.NoError(t, st.SetOwner(ctx, "d_a2", a.ID))
	require.NoError(t, st.SetOwner(ctx, "d_b1", b.ID))
	return st, dm, a, b
}

func TestResolveOnlyOwnDevices(t *testing.T) {
	st, dm, a, b := fixture(t)
	r := NewResolver(dm, st)
	ctxA := auth.WithUser(context.Background(), a)
	ctxB := auth.WithUser(context.Background(), b)

	inst, id, err := r.Resolve(ctxA, "d_a1")
	require.NoError(t, err)
	require.Equal(t, "d_a1", id)
	require.Equal(t, "d_a1", inst.ID())

	_, id, err = r.Resolve(ctxA, "d_b1")
	require.Error(t, err, "another user's device")
	require.Equal(t, "d_b1", id, "reported like a missing device, so the middleware answers 404")

	_, _, err = r.Resolve(ctxA, "d_orphan")
	require.Error(t, err, "a device nobody owns is invisible")

	_, _, err = r.Resolve(ctxA, "")
	require.ErrorIs(t, err, ErrDeviceIDRequired, "two devices and no id: ask, never guess")

	inst, id, err = r.Resolve(ctxB, "")
	require.NoError(t, err, "one device: it is the default")
	require.Equal(t, "d_b1", id)
	require.Equal(t, "d_b1", inst.ID())

	_, _, err = r.Resolve(context.Background(), "d_a1")
	require.Error(t, err, "no user in context")
}

func TestResolveUserWithNoDevices(t *testing.T) {
	st, dm, _, _ := fixture(t)
	c, _ := st.CreateUser(context.Background(), "+27825550102")
	_, id, err := NewResolver(dm, st).Resolve(auth.WithUser(context.Background(), c), "")
	require.ErrorIs(t, err, ErrNoDeviceLinked)
	require.Empty(t, id)
}
```

```go
// src/multiuser/owners/devices_test.go
package owners

import (
	"context"
	"testing"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/stretchr/testify/require"
)

// innerStub records calls; every method succeeds.
type innerStub struct {
	added, removed, touched []string
	all                     []domainDevice.Device
}

func (s *innerStub) ListDevices(context.Context) ([]domainDevice.Device, error) { return s.all, nil }
func (s *innerStub) GetDevice(_ context.Context, id string) (*domainDevice.Device, error) {
	s.touched = append(s.touched, id)
	return &domainDevice.Device{ID: id}, nil
}
func (s *innerStub) AddDevice(_ context.Context, id string, _ *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	s.added = append(s.added, id)
	return &domainDevice.Device{ID: id}, nil
}
func (s *innerStub) RemoveDevice(_ context.Context, id string) error { s.removed = append(s.removed, id); return nil }
func (s *innerStub) LoginDevice(_ context.Context, id string) (domainApp.LoginResponse, error) {
	s.touched = append(s.touched, id)
	return domainApp.LoginResponse{}, nil
}
func (s *innerStub) LoginDeviceWithCode(_ context.Context, id, _ string) (string, error) {
	s.touched = append(s.touched, id)
	return "", nil
}
func (s *innerStub) LogoutDevice(_ context.Context, id string) error    { s.touched = append(s.touched, id); return nil }
func (s *innerStub) ReconnectDevice(_ context.Context, id string) error { s.touched = append(s.touched, id); return nil }
func (s *innerStub) GetStatus(_ context.Context, id string) (bool, bool, error) {
	s.touched = append(s.touched, id)
	return true, true, nil
}
func (s *innerStub) SetDeviceWebhook(_ context.Context, id, _ string) error {
	s.touched = append(s.touched, id)
	return nil
}
func (s *innerStub) GetDeviceWebhook(_ context.Context, id string) (string, error) {
	s.touched = append(s.touched, id)
	return "", nil
}
func (s *innerStub) SetDeviceWebhookConfig(_ context.Context, id string, _ *chatstorage.DeviceWebhookConfig) error {
	s.touched = append(s.touched, id)
	return nil
}
func (s *innerStub) GetDeviceWebhookConfig(_ context.Context, id string) (*chatstorage.DeviceWebhookConfig, error) {
	s.touched = append(s.touched, id)
	return nil, nil
}

func TestScopedDevicesIsolation(t *testing.T) {
	st, _, a, b := fixture(t)
	inner := &innerStub{all: []domainDevice.Device{{ID: "d_a1"}, {ID: "d_a2"}, {ID: "d_b1"}, {ID: "d_orphan"}}}
	svc := NewScopedDevices(inner, st)
	ctxA := auth.WithUser(context.Background(), a)

	list, err := svc.ListDevices(ctxA)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"d_a1", "d_a2"}, []string{list[0].ID, list[1].ID})
	require.Len(t, list, 2)

	calls := map[string]func() error{
		"GetDevice":              func() error { _, err := svc.GetDevice(ctxA, "d_b1"); return err },
		"RemoveDevice":           func() error { return svc.RemoveDevice(ctxA, "d_b1") },
		"LoginDevice":            func() error { _, err := svc.LoginDevice(ctxA, "d_b1"); return err },
		"LoginDeviceWithCode":    func() error { _, err := svc.LoginDeviceWithCode(ctxA, "d_b1", "123"); return err },
		"LogoutDevice":           func() error { return svc.LogoutDevice(ctxA, "d_b1") },
		"ReconnectDevice":        func() error { return svc.ReconnectDevice(ctxA, "d_b1") },
		"GetStatus":              func() error { _, _, err := svc.GetStatus(ctxA, "d_b1"); return err },
		"SetDeviceWebhook":       func() error { return svc.SetDeviceWebhook(ctxA, "d_b1", "https://x") },
		"GetDeviceWebhook":       func() error { _, err := svc.GetDeviceWebhook(ctxA, "d_b1"); return err },
		"SetDeviceWebhookConfig": func() error { return svc.SetDeviceWebhookConfig(ctxA, "d_b1", nil) },
		"GetDeviceWebhookConfig": func() error { _, err := svc.GetDeviceWebhookConfig(ctxA, "d_b1"); return err },
	}
	for name, call := range calls {
		require.ErrorIs(t, call(), pkgError.ErrDeviceNotFound, name)
	}
	require.Empty(t, inner.touched, "the inner usecase never saw b's device")
	require.Empty(t, inner.removed)
	_ = b
}

func TestAddDeviceGeneratesIDOwnsItAndEnforcesLimit(t *testing.T) {
	st, _, a, _ := fixture(t) // a already owns 2 devices; MaxDevices defaults to 2
	inner := &innerStub{}
	svc := NewScopedDevices(inner, st)
	_, err := svc.AddDevice(auth.WithUser(context.Background(), a), "chosen-by-user", nil)
	require.Error(t, err, "limit reached")
	require.Empty(t, inner.added)

	c, _ := st.CreateUser(context.Background(), "+27825550102")
	dev, err := svc.AddDevice(auth.WithUser(context.Background(), c), "chosen-by-user", nil)
	require.NoError(t, err)
	require.Regexp(t, `^d_[0-9a-f]{32}$`, dev.ID, "server picks the id; the requested id is ignored")
	owner, err := st.OwnerOf(context.Background(), dev.ID)
	require.NoError(t, err)
	require.Equal(t, c.ID, owner)

	require.NoError(t, svc.RemoveDevice(auth.WithUser(context.Background(), c), dev.ID))
	_, err = st.OwnerOf(context.Background(), dev.ID)
	require.Error(t, err, "ownership row removed with the device")
}
```

```go
// src/multiuser/owners/duplicate_test.go
package owners

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakePairs struct {
	devices   []PairedDevice
	loggedOut []string
}

func (f *fakePairs) Paired() []PairedDevice { return f.devices }
func (f *fakePairs) Logout(_ context.Context, id string) error {
	f.loggedOut = append(f.loggedOut, id)
	return nil
}

func TestSecondUserCannotLinkTheSameNumber(t *testing.T) {
	st, _, _, _ := fixture(t) // d_a1 is a's, d_b1 is b's
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_b1", JID: "27825550999@s.whatsapp.net"}, // b just scanned with a's phone
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_b1")
	require.NoError(t, err)
	require.True(t, refused)
	require.Equal(t, []string{"d_b1"}, src.loggedOut, "the newcomer is logged out; the first owner keeps it")
}

func TestSameUserRelinkIsAllowed(t *testing.T) {
	st, _, _, _ := fixture(t) // d_a1 and d_a2 are both a's
	src := &fakePairs{devices: []PairedDevice{
		{ID: "d_a1", JID: "27825550999@s.whatsapp.net"},
		{ID: "d_a2", JID: "27825550999@s.whatsapp.net"},
	}}
	refused, err := GuardDuplicatePhone(context.Background(), src, st, "d_a2")
	require.NoError(t, err)
	require.False(t, refused)
	require.Empty(t, src.loggedOut)
}
```

- [ ] **Step 2: Run them and watch them fail.** Run `cd src && go test ./multiuser/owners/`. Expected: FAIL (package missing).

- [ ] **Step 3: Implement.**

```go
// src/multiuser/owners/resolver.go
// Package owners enforces that a user only ever reaches WhatsApp accounts they own.
package owners

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
)

var (
	ErrDeviceIDRequired = pkgError.ValidationError("you have several WhatsApp accounts linked; pass device_id or the X-Device-Id header")
	ErrNoDeviceLinked   = pkgError.ValidationError("link a WhatsApp account first")
	errNoUser           = errors.New("no signed-in user")
)

type DeviceGetter interface {
	GetDevice(id string) (*whatsapp.DeviceInstance, bool)
}

type Resolver struct {
	dm DeviceGetter
	st *store.Store
}

func NewResolver(dm DeviceGetter, st *store.Store) *Resolver { return &Resolver{dm: dm, st: st} }

// Resolve mirrors whatsapp.DeviceManager.ResolveDevice's contract. A device
// that exists but belongs to someone else is reported exactly like one that
// does not exist.
func (r *Resolver) Resolve(ctx context.Context, deviceID string) (*whatsapp.DeviceInstance, string, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, deviceID, errNoUser
	}
	if deviceID == "" {
		ids, err := r.st.DevicesOf(ctx, u.ID)
		if err != nil {
			return nil, "", err
		}
		switch len(ids) {
		case 0:
			return nil, "", ErrNoDeviceLinked
		case 1:
			deviceID = ids[0]
		default:
			return nil, "", ErrDeviceIDRequired
		}
	} else {
		owner, err := r.st.OwnerOf(ctx, deviceID)
		if err != nil || owner != u.ID {
			return nil, deviceID, pkgError.ErrDeviceNotFound
		}
	}
	inst, ok := r.dm.GetDevice(deviceID)
	if !ok {
		return nil, deviceID, pkgError.ErrDeviceNotFound
	}
	return inst, deviceID, nil
}

func NewDeviceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return "d_" + hex.EncodeToString(b)
}
```

```go
// src/multiuser/owners/devices.go
package owners

import (
	"context"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
)

// ScopedDevices wraps upstream's device usecase so the unchanged
// /devices REST handlers only ever see the caller's own devices.
type ScopedDevices struct {
	inner domainDevice.IDeviceUsecase
	st    *store.Store
}

func NewScopedDevices(inner domainDevice.IDeviceUsecase, st *store.Store) domainDevice.IDeviceUsecase {
	return &ScopedDevices{inner: inner, st: st}
}

func (s *ScopedDevices) own(ctx context.Context, deviceID string) error {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return pkgError.ErrDeviceNotFound
	}
	owner, err := s.st.OwnerOf(ctx, deviceID)
	if err != nil || owner != u.ID {
		return pkgError.ErrDeviceNotFound
	}
	return nil
}

func (s *ScopedDevices) ListDevices(ctx context.Context) ([]domainDevice.Device, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return []domainDevice.Device{}, nil
	}
	ids, err := s.st.DevicesOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	mine := make(map[string]bool, len(ids))
	for _, id := range ids {
		mine[id] = true
	}
	all, err := s.inner.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := []domainDevice.Device{}
	for _, d := range all {
		if mine[d.ID] {
			out = append(out, d)
		}
	}
	return out, nil
}

func (s *ScopedDevices) AddDevice(ctx context.Context, _ string, webhook *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	u, ok := auth.UserFrom(ctx)
	if !ok {
		return nil, pkgError.ErrDeviceNotFound
	}
	ids, err := s.st.DevicesOf(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if len(ids) >= u.MaxDevices {
		return nil, pkgError.ValidationError("you have reached your limit of linked WhatsApp accounts")
	}
	id := NewDeviceID()
	dev, err := s.inner.AddDevice(ctx, id, webhook)
	if err != nil {
		return nil, err
	}
	if err := s.st.SetOwner(ctx, id, u.ID); err != nil {
		_ = s.inner.RemoveDevice(ctx, id) // never leave an ownerless device behind
		return nil, err
	}
	_ = s.st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: "device_add", DeviceID: id})
	return dev, nil
}

func (s *ScopedDevices) RemoveDevice(ctx context.Context, deviceID string) error {
	if err := s.own(ctx, deviceID); err != nil {
		return err
	}
	if err := s.inner.RemoveDevice(ctx, deviceID); err != nil {
		return err
	}
	u, _ := auth.UserFrom(ctx)
	_ = s.st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: "device_remove", DeviceID: deviceID})
	return s.st.RemoveOwner(ctx, deviceID)
}

func (s *ScopedDevices) GetDevice(ctx context.Context, id string) (*domainDevice.Device, error) {
	if err := s.own(ctx, id); err != nil {
		return nil, err
	}
	return s.inner.GetDevice(ctx, id)
}

func (s *ScopedDevices) LoginDevice(ctx context.Context, id string) (domainApp.LoginResponse, error) {
	if err := s.own(ctx, id); err != nil {
		return domainApp.LoginResponse{}, err
	}
	return s.inner.LoginDevice(ctx, id)
}

func (s *ScopedDevices) LoginDeviceWithCode(ctx context.Context, id, phone string) (string, error) {
	if err := s.own(ctx, id); err != nil {
		return "", err
	}
	return s.inner.LoginDeviceWithCode(ctx, id, phone)
}

func (s *ScopedDevices) LogoutDevice(ctx context.Context, id string) error {
	if err := s.own(ctx, id); err != nil {
		return err
	}
	return s.inner.LogoutDevice(ctx, id)
}

func (s *ScopedDevices) ReconnectDevice(ctx context.Context, id string) error {
	if err := s.own(ctx, id); err != nil {
		return err
	}
	return s.inner.ReconnectDevice(ctx, id)
}

func (s *ScopedDevices) GetStatus(ctx context.Context, id string) (bool, bool, error) {
	if err := s.own(ctx, id); err != nil {
		return false, false, err
	}
	return s.inner.GetStatus(ctx, id)
}

func (s *ScopedDevices) SetDeviceWebhook(ctx context.Context, id, url string) error {
	if err := s.own(ctx, id); err != nil {
		return err
	}
	return s.inner.SetDeviceWebhook(ctx, id, url)
}

func (s *ScopedDevices) GetDeviceWebhook(ctx context.Context, id string) (string, error) {
	if err := s.own(ctx, id); err != nil {
		return "", err
	}
	return s.inner.GetDeviceWebhook(ctx, id)
}

func (s *ScopedDevices) SetDeviceWebhookConfig(ctx context.Context, id string, cfg *chatstorage.DeviceWebhookConfig) error {
	if err := s.own(ctx, id); err != nil {
		return err
	}
	return s.inner.SetDeviceWebhookConfig(ctx, id, cfg)
}

func (s *ScopedDevices) GetDeviceWebhookConfig(ctx context.Context, id string) (*chatstorage.DeviceWebhookConfig, error) {
	if err := s.own(ctx, id); err != nil {
		return nil, err
	}
	return s.inner.GetDeviceWebhookConfig(ctx, id)
}
```

```go
// src/multiuser/owners/duplicate.go
package owners

import (
	"context"
	"strings"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
)

type PairedDevice struct{ ID, JID string }

type PairSource interface {
	Paired() []PairedDevice
	Logout(ctx context.Context, deviceID string) error
}

// GuardDuplicatePhone runs after a device pairs. If another user's device is
// already linked to the same WhatsApp number, the new link is logged out.
func GuardDuplicatePhone(ctx context.Context, src PairSource, st *store.Store, deviceID string) (bool, error) {
	newOwner, err := st.OwnerOf(ctx, deviceID)
	if err != nil {
		return false, nil // not a WTF-owned device; nothing to guard
	}
	var number string
	for _, d := range src.Paired() {
		if d.ID == deviceID {
			number = userPart(d.JID)
		}
	}
	if number == "" {
		return false, nil
	}
	for _, d := range src.Paired() {
		if d.ID == deviceID || userPart(d.JID) != number {
			continue
		}
		if owner, err := st.OwnerOf(ctx, d.ID); err == nil && owner != newOwner {
			_ = st.Audit(ctx, store.AuditEvent{UserID: newOwner, Action: "device_link_refused_duplicate", DeviceID: deviceID})
			return true, src.Logout(ctx, deviceID)
		}
	}
	return false, nil
}

// userPart turns "27825550999:12@s.whatsapp.net" into "27825550999".
func userPart(jid string) string {
	u := jid
	if i := strings.IndexByte(u, '@'); i >= 0 {
		u = u[:i]
	}
	if i := strings.IndexByte(u, ':'); i >= 0 {
		u = u[:i]
	}
	return u
}
```

- [ ] **Step 4: Run the tests.** Run `cd src && go test ./multiuser/owners/ -v`. Expected: PASS. If `whatsapp.NewDeviceInstance(id, nil, nil)` panics on a nil client, construct the instance through `whatsapp.NewDeviceManager(nil, nil, nil).CreateDevice(ctx, id)` instead. Check `device_instance.go:33` first.

- [ ] **Step 5: Break-it check.** In `ScopedDevices.own`, change `owner != u.ID` to `false`. `TestScopedDevicesIsolation` must fail. Then revert.

- [ ] **Step 6: Commit.**

```bash
git add src/multiuser/owners
git commit -m "feat(wtf): owner-checked device resolution, scoped device usecase, duplicate-number guard"
```

---

### Task 5: Upstream hooks (device resolver, event hook, device-scoped message lookups)

**Files:**
- Modify: `src/ui/rest/middleware/device.go` (hook var + one call site)
- Modify: `src/infrastructure/whatsapp/event_handler.go` (hook var + call; scoped DeleteForMe lookup)
- Modify: `src/usecase/message.go:274,330,619` (scoped `GetMessageByID`)
- Modify: `src/usecase/send.go:2085` (scoped `GetChat`)
- Test: `src/ui/rest/middleware/device_hook_test.go`, `src/usecase/message_scope_test.go`

**Interfaces:**
- Produces:
  - `middleware.DeviceResolveFunc func(c fiber.Ctx, deviceID string) (*whatsapp.DeviceInstance, string, error)`: nil means upstream behaviour
  - `whatsapp.EventHook func(ctx context.Context, instance *DeviceInstance, evt any)`: nil means no-op; called first in `handler()`

- [ ] **Step 1: Write the failing tests.**

```go
// src/ui/rest/middleware/device_hook_test.go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestDeviceResolveFuncOverridesManager(t *testing.T) {
	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	_, err := dm.CreateDevice(t.Context(), "upstream-default")
	require.NoError(t, err)

	saved := DeviceResolveFunc
	t.Cleanup(func() { DeviceResolveFunc = saved })
	var asked string
	DeviceResolveFunc = func(_ fiber.Ctx, id string) (*whatsapp.DeviceInstance, string, error) {
		asked = id
		return nil, id, pkgError.ErrDeviceNotFound
	}

	app := fiber.New()
	app.Get("/x", DeviceMiddleware(dm), func(c fiber.Ctx) error { return c.SendString("reached") })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(DeviceIDHeader, "someone-elses")
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, "someone-elses", asked)

	// With no header the hook decides too: upstream's "only device" fallback is never used.
	resp, err = app.Test(httptest.NewRequest(http.MethodGet, "/x", nil))
	require.NoError(t, err)
	require.NotEqual(t, http.StatusOK, resp.StatusCode)
}
```

For the message scoping, write a test against `serviceMessage.DownloadMedia`'s lookup. It needs a WhatsApp client, so instead test the repository-facing seam: add a small unexported helper `scopedMessageLookup(repo, ctx, id)` in `usecase/message.go` that all three call sites use, and test that.

```go
// src/usecase/message_scope_test.go
package usecase

import (
	"context"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/stretchr/testify/require"
)

type lookupRepo struct {
	domainChatStorage.IChatStorageRepository
	byIDCalled         bool
	byDeviceID, byID   string
}

func (r *lookupRepo) GetMessageByID(id string) (*domainChatStorage.Message, error) {
	r.byIDCalled = true
	return &domainChatStorage.Message{ID: id, DeviceID: "someone-else"}, nil
}
func (r *lookupRepo) GetMessageByIDAndDevice(deviceID, id string) (*domainChatStorage.Message, error) {
	r.byDeviceID, r.byID = deviceID, id
	return nil, nil
}

func TestMessageLookupsAreDeviceScoped(t *testing.T) {
	repo := &lookupRepo{}
	inst := whatsapp.NewDeviceInstance("d_a1", nil, nil)
	ctx := whatsapp.ContextWithDevice(context.Background(), inst)
	msg, err := scopedMessageLookup(ctx, repo, "MSG1")
	require.NoError(t, err)
	require.Nil(t, msg, "another device's row is never returned")
	require.False(t, repo.byIDCalled, "the unscoped lookup is not used")
	require.Equal(t, "MSG1", repo.byID)
	require.Equal(t, "d_a1", repo.byDeviceID)
}
```

- [ ] **Step 2: Run them and watch them fail.** Run `cd src && go test ./ui/rest/middleware/ ./usecase/ -run 'DeviceResolveFunc|DeviceScoped'`. Expected: FAIL (undefined `DeviceResolveFunc`, `scopedMessageLookup`).

- [ ] **Step 3: Implement the middleware hook.** In `src/ui/rest/middleware/device.go`, add above `DeviceMiddleware`:

```go
// WTF: DeviceResolveFunc lets the multi-user layer replace how a request
// picks its device (owner-checked, no "default device" fallback). Nil keeps
// upstream behaviour.
var DeviceResolveFunc func(c fiber.Ctx, deviceID string) (*whatsapp.DeviceInstance, string, error)
```

Replace `instance, resolvedID, err := dm.ResolveDevice(deviceID)` with:

```go
		var (
			instance   *whatsapp.DeviceInstance
			resolvedID string
			err        error
		)
		if DeviceResolveFunc != nil { // WTF
			instance, resolvedID, err = DeviceResolveFunc(c, deviceID)
		} else {
			instance, resolvedID, err = dm.ResolveDevice(deviceID)
		}
```

The existing error branch still maps "id given" to a 404 and "no id" to a 400. One change is needed for the multi-user errors: when `err` is a `pkgError.GenericError` and no id was given, answer with its status and message instead of the generic `DEVICE_ID_REQUIRED`. Insert before the existing `return c.Status(fiber.StatusBadRequest)...`:

```go
			var generic pkgError.GenericError
			if errors.As(err, &generic) { // WTF: e.g. "link a WhatsApp account first"
				return c.Status(generic.StatusCode()).JSON(utils.ResponseData{
					Status: generic.StatusCode(), Code: generic.ErrCode(), Message: generic.Error(),
				})
			}
```

Add the imports `errors` and `pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"`.

- [ ] **Step 4: Implement the event hook.** In `src/infrastructure/whatsapp/event_handler.go`, add above `handler`:

```go
// WTF: EventHook sees every event before upstream handling. The multi-user
// layer uses it for the service number's sign-in messages and the
// duplicate-number guard. It must return quickly; long work goes in a goroutine.
var EventHook func(ctx context.Context, instance *DeviceInstance, evt any)
```

In `handler`, right after `ctx = ContextWithDevice(ctx, instance)`:

```go
	if EventHook != nil { // WTF
		EventHook(ctx, instance, rawEvt)
	}
```

In `handleDeleteForMe`, replace `chatStorageRepo.GetMessageByID(evt.MessageID)` with `chatStorageRepo.GetMessageByIDAndDevice("", evt.MessageID)`. The `chatStorageRepo` here is the per-device wrapper (`instance.GetChatStorage()`), whose `GetMessageByIDAndDevice` fills in its own device id when given `""` (see `chatstorage_wrapper.go`). Today one account's "delete for me" can delete another account's copy of the same message id. This fixes that.

- [ ] **Step 5: Implement scoped message lookups.** In `src/usecase/message.go`, add:

```go
// WTF: scopedMessageLookup finds a message only among the calling device's
// rows. The unscoped GetMessageByID let one account read another account's
// media keys by message id.
func scopedMessageLookup(ctx context.Context, repo domainChatStorage.IChatStorageRepository, messageID string) (*domainChatStorage.Message, error) {
	deviceID := deviceIDFromContext(ctx)
	if client := whatsapp.ClientFromContext(ctx); client != nil && client.Store != nil && client.Store.ID != nil {
		deviceID = client.Store.ID.ToNonAD().String()
	}
	if deviceID == "" {
		return nil, nil
	}
	return repo.GetMessageByIDAndDevice(deviceID, messageID)
}
```

Before relying on it, check which id the `messages.device_id` column holds: the slot id, or the store JID. AGENTS.md says storage uses `client.Store.ID.ToNonAD().String()` after login, and `deviceIDFromContext` (chat.go:263) returns `inst.JID()` first. Read `CreateMessage` in `sqlite_repository.go` to confirm which value is written. Then make the helper use exactly that value, and adjust the test's expected `byDeviceID` to match (for a nil-client instance, `inst.JID()` is empty and `inst.ID()` is used).

Replace the three `service.chatStorageRepo.GetMessageByID(request.MessageID)` calls (lines 274, 330, 619) with `scopedMessageLookup(ctx, service.chatStorageRepo, request.MessageID)`. Line 330's comment claims cross-device lookup is harmless. Replace that comment with: `// WTF: scoped to this device; a cross-device lookup leaks whether another account holds this message.`

In `src/usecase/send.go` `getDefaultEphemeralExpiration`, the repo is the base repository and `GetChat(jid)` is unscoped. Change the signature to take `ctx context.Context`, look the chat up with `service.chatStorageRepo.GetChatByDevice(deviceIDFromContext(ctx), jid)`, and update its callers (`grep -n getDefaultEphemeralExpiration src/usecase/*.go`).

- [ ] **Step 6: Run the tests.** Run `cd src && go test ./ui/rest/middleware/ ./usecase/ ./infrastructure/whatsapp/`. Expected: PASS (plus the known ffmpeg failure).

- [ ] **Step 7: Commit.**

```bash
git add src/ui/rest/middleware src/infrastructure/whatsapp/event_handler.go src/usecase
git commit -m "feat(wtf): resolver and event hooks; scope message and chat lookups to the calling device"
```

---

### Task 6: WhatsApp sign-in through the service number

**Files:**
- Create: `src/multiuser/signin/ratelimit.go`, `src/multiuser/signin/handler.go`
- Test: `src/multiuser/signin/ratelimit_test.go`, `src/multiuser/signin/handler_test.go`

**Interfaces:**
- Consumes: `store.*` (Tasks 1 and 2), `whatsapp.DeviceInstance`, `whatsapp.NormalizeJIDFromLID`, whatsmeow `events.Message`, `types.JID`, `waE2E.Message`.
- Produces:
  - `signin.Phrase = "Sign me in to"`: the prefilled wa.me text is `Phrase + " " + ProductName`
  - `signin.NewLimiter(max int, window time.Duration) *Limiter`, `(*Limiter).Allow(key string, now time.Time) bool`
  - `signin.Sender interface{ SendText(ctx context.Context, inst *whatsapp.DeviceInstance, to types.JID, text string) error }`
  - `signin.ClientSender{}`: the real `Sender` using `inst.GetClient().SendMessage`
  - `signin.Handler{Store *store.Store; Send Sender; ProductName, PublicURL string; SignupOpen bool; IsSuperadmin func(phone string) bool; Limiter *Limiter; ResolvePN func(ctx context.Context, inst *whatsapp.DeviceInstance, jid types.JID) types.JID; Now func() time.Time}`
  - `(*Handler).Handle(ctx context.Context, inst *whatsapp.DeviceInstance, evt *events.Message)`: synchronous, for tests
  - `(*Handler).OnEvent(ctx context.Context, inst *whatsapp.DeviceInstance, evt any)`: for `whatsapp.EventHook`. It runs `Handle` in a goroutine with a 20-second timeout, only for the service device.

- [ ] **Step 1: Write the failing tests.**

```go
// src/multiuser/signin/ratelimit_test.go
package signin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLimiterWindow(t *testing.T) {
	l := NewLimiter(3, time.Hour)
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		require.True(t, l.Allow("+27825550100", now))
	}
	require.False(t, l.Allow("+27825550100", now))
	require.True(t, l.Allow("+27825550101", now), "per key")
	require.True(t, l.Allow("+27825550100", now.Add(61*time.Minute)), "window resets")
}
```

```go
// src/multiuser/signin/handler_test.go
package signin

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

type sent struct {
	to   types.JID
	text string
}
type recorder struct{ msgs []sent }

func (r *recorder) SendText(_ context.Context, _ *whatsapp.DeviceInstance, to types.JID, text string) error {
	r.msgs = append(r.msgs, sent{to, text})
	return nil
}

func newHandler(t *testing.T, open bool) (*Handler, *recorder, *store.Store) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	rec := &recorder{}
	return &Handler{
		Store: st, Send: rec, ProductName: "WTF", PublicURL: "https://wtf.onrender.com", SignupOpen: open,
		IsSuperadmin: func(p string) bool { return p == "+27825550100" },
		Limiter:      NewLimiter(3, time.Hour),
		ResolvePN:    func(_ context.Context, _ *whatsapp.DeviceInstance, j types.JID) types.JID { return j },
		Now:          func() time.Time { return time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC) },
	}, rec, st
}

func msg(sender types.JID, text string) *events.Message {
	return &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: sender, Sender: sender},
			ID:            "M1",
		},
		Message: &waE2E.Message{Conversation: proto.String(text)},
	}
}

var svc = whatsapp.NewDeviceInstance("d_service", nil, nil)

func TestKnownUserGetsALink(t *testing.T) {
	h, rec, st := newHandler(t, false)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	from := types.NewJID("27825550101", types.DefaultUserServer)
	h.Handle(context.Background(), svc, msg(from, "Sign me in to WTF"))
	require.Len(t, rec.msgs, 1)
	require.Equal(t, from, rec.msgs[0].to)
	require.Contains(t, rec.msgs[0].text, "https://wtf.onrender.com/signin/verify?t=")
	raw := rec.msgs[0].text[strings.Index(rec.msgs[0].text, "t=")+2:]
	raw = strings.Fields(raw)[0]
	phone, err := st.ConsumeLoginToken(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, "+27825550101", phone)
}

func TestClosedSignupRefusesStrangersButAdmitsSuperadmin(t *testing.T) {
	h, rec, st := newHandler(t, false)
	h.Handle(context.Background(), svc, msg(types.NewJID("27825550199", types.DefaultUserServer), "sign me in to wtf"))
	require.Len(t, rec.msgs, 1)
	require.NotContains(t, rec.msgs[0].text, "/signin/verify")
	_, err := st.UserByPhone(context.Background(), "+27825550199")
	require.ErrorIs(t, err, store.ErrNotFound, "no account created")

	h.Handle(context.Background(), svc, msg(types.NewJID("27825550100", types.DefaultUserServer), "Sign me in to WTF"))
	require.Len(t, rec.msgs, 2)
	require.Contains(t, rec.msgs[1].text, "/signin/verify?t=")
	_, err = st.UserByPhone(context.Background(), "+27825550100")
	require.NoError(t, err, "superadmin created on first contact")
}

func TestOpenSignupCreatesUser(t *testing.T) {
	h, rec, st := newHandler(t, true)
	h.Handle(context.Background(), svc, msg(types.NewJID("27825550199", types.DefaultUserServer), "Sign me in to WTF"))
	require.Contains(t, rec.msgs[0].text, "/signin/verify?t=")
	_, err := st.UserByPhone(context.Background(), "+27825550199")
	require.NoError(t, err)
}

func TestIgnoredMessages(t *testing.T) {
	h, rec, st := newHandler(t, true)
	from := types.NewJID("27825550101", types.DefaultUserServer)
	h.Handle(context.Background(), svc, msg(from, "hello there")) // not the phrase
	group := msg(from, "Sign me in to WTF")
	group.Info.IsGroup = true
	group.Info.Chat = types.NewJID("120363000000000000", types.GroupServer)
	h.Handle(context.Background(), svc, group)
	mine := msg(from, "Sign me in to WTF")
	mine.Info.IsFromMe = true
	h.Handle(context.Background(), svc, mine)
	require.Empty(t, rec.msgs)
	n, _ := st.ListUsers(context.Background())
	require.Empty(t, n)
}

func TestUnresolvableLIDIsRefusedWithoutStoringAnything(t *testing.T) {
	h, rec, st := newHandler(t, true)
	lid := types.NewJID("123456789012345", types.HiddenUserServer)
	h.Handle(context.Background(), svc, msg(lid, "Sign me in to WTF"))
	require.Len(t, rec.msgs, 1)
	require.Contains(t, rec.msgs[0].text, "couldn't read your number")
	users, _ := st.ListUsers(context.Background())
	require.Empty(t, users)
}

func TestDisabledUserIsTold(t *testing.T) {
	h, rec, st := newHandler(t, true)
	u, _ := st.CreateUser(context.Background(), "+27825550101")
	_ = st.SetUserStatus(context.Background(), u.ID, store.StatusDisabled)
	h.Handle(context.Background(), svc, msg(types.NewJID("27825550101", types.DefaultUserServer), "Sign me in to WTF"))
	require.Contains(t, rec.msgs[0].text, "disabled")
	require.NotContains(t, rec.msgs[0].text, "/signin/verify")
}

func TestRateLimitIsSilent(t *testing.T) {
	h, rec, st := newHandler(t, true)
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	from := types.NewJID("27825550101", types.DefaultUserServer)
	for i := 0; i < 5; i++ {
		h.Handle(context.Background(), svc, msg(from, "Sign me in to WTF"))
	}
	require.Len(t, rec.msgs, 3, "no reply past the limit, so we never amplify spam")
}

func TestOnEventOnlyServesTheServiceDevice(t *testing.T) {
	h, rec, st := newHandler(t, true)
	h.ServiceDeviceID = func(context.Context) string { return "d_service" }
	_, _ = st.CreateUser(context.Background(), "+27825550101")
	userPhone := whatsapp.NewDeviceInstance("d_users_own_phone", nil, nil)
	h.OnEvent(context.Background(), userPhone, msg(types.NewJID("27825550101", types.DefaultUserServer), "Sign me in to WTF"))
	time.Sleep(50 * time.Millisecond)
	require.Empty(t, rec.msgs, "a user's own linked phone never answers sign-in messages")
}
```

The handler gets one more field from this last test: `ServiceDeviceID func(ctx context.Context) string`.

- [ ] **Step 2: Run them and watch them fail.** Run `cd src && go test ./multiuser/signin/`. Expected: FAIL (package missing).

- [ ] **Step 3: Implement.**

```go
// src/multiuser/signin/ratelimit.go
package signin

import (
	"sync"
	"time"
)

// Limiter is a per-key fixed window, in memory. Enough for one instance.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*windowCount
}

type windowCount struct {
	start time.Time
	n     int
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, hits: map[string]*windowCount{}}
}

func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[key] = &windowCount{start: now, n: 1}
		if len(l.hits) > 10000 { // bound memory: drop stale windows
			for k, v := range l.hits {
				if now.Sub(v.start) >= l.window {
					delete(l.hits, k)
				}
			}
		}
		return true
	}
	if w.n >= l.max {
		return false
	}
	w.n++
	return true
}
```

```go
// src/multiuser/signin/handler.go
// Package signin turns "Sign me in to WTF" messages on the service number into
// single-use sign-in links sent back in the same chat.
package signin

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const Phrase = "Sign me in to"

const tokenTTL = 15 * time.Minute

type Sender interface {
	SendText(ctx context.Context, inst *whatsapp.DeviceInstance, to types.JID, text string) error
}

type ClientSender struct{}

func (ClientSender) SendText(ctx context.Context, inst *whatsapp.DeviceInstance, to types.JID, text string) error {
	client := inst.GetClient()
	if client == nil {
		return errors.New("service number not connected")
	}
	_, err := client.SendMessage(ctx, to, &waE2E.Message{Conversation: proto.String(text)})
	return err
}

type Handler struct {
	Store           *store.Store
	Send            Sender
	ProductName     string
	PublicURL       string
	SignupOpen      bool
	IsSuperadmin    func(phone string) bool
	Limiter         *Limiter
	ResolvePN       func(ctx context.Context, inst *whatsapp.DeviceInstance, jid types.JID) types.JID
	ServiceDeviceID func(ctx context.Context) string
	Now             func() time.Time
}

func DefaultResolvePN(ctx context.Context, inst *whatsapp.DeviceInstance, jid types.JID) types.JID {
	return whatsapp.NormalizeJIDFromLID(ctx, jid, inst.GetClient())
}

func (h *Handler) OnEvent(ctx context.Context, inst *whatsapp.DeviceInstance, evt any) {
	m, ok := evt.(*events.Message)
	if !ok || inst == nil || h.ServiceDeviceID == nil || inst.ID() != h.ServiceDeviceID(ctx) {
		return
	}
	go func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		h.Handle(c, inst, m)
	}()
}

func messageText(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	if t := m.GetConversation(); t != "" {
		return t
	}
	return m.GetExtendedTextMessage().GetText()
}

func (h *Handler) Handle(ctx context.Context, inst *whatsapp.DeviceInstance, evt *events.Message) {
	info := evt.Info
	if info.IsFromMe || info.IsGroup || info.Chat.Server == types.GroupServer ||
		info.Chat.Server == types.BroadcastServer || info.Chat.Server == types.NewsletterServer {
		return
	}
	if !strings.Contains(strings.ToLower(messageText(evt.Message)), strings.ToLower(Phrase)) {
		return
	}

	sender := info.Sender
	if sender.Server == types.HiddenUserServer && !info.SenderAlt.IsEmpty() && info.SenderAlt.Server == types.DefaultUserServer {
		sender = info.SenderAlt
	}
	pn := h.ResolvePN(ctx, inst, sender)
	if pn.Server != types.DefaultUserServer {
		h.reply(ctx, inst, info.Chat, "Sorry, we couldn't read your number from this chat. Please update WhatsApp and try again.")
		return
	}
	phone, err := store.NormalizePhone(pn.User)
	if err != nil {
		return
	}
	if !h.Limiter.Allow(phone, h.Now()) {
		return // silent: never amplify
	}

	user, err := h.Store.UserByPhone(ctx, phone)
	switch {
	case errors.Is(err, store.ErrNotFound):
		if !h.SignupOpen && !h.IsSuperadmin(phone) {
			h.reply(ctx, inst, info.Chat, fmt.Sprintf("%s isn't open for sign-ups yet. We'll let you know when it is.", h.ProductName))
			return
		}
		if user, err = h.Store.CreateUser(ctx, phone); err != nil {
			logrus.WithError(err).Error("[WTF] sign-in: create user failed")
			return
		}
		_ = h.Store.Audit(ctx, store.AuditEvent{UserID: user.ID, Action: "signup"})
	case err != nil:
		logrus.WithError(err).Error("[WTF] sign-in: user lookup failed")
		return
	}
	if user.Status != store.StatusActive {
		h.reply(ctx, inst, info.Chat, fmt.Sprintf("This %s account is disabled.", h.ProductName))
		return
	}

	raw, err := h.Store.CreateLoginToken(ctx, phone, tokenTTL)
	if err != nil {
		logrus.WithError(err).Error("[WTF] sign-in: token failed")
		return
	}
	_ = h.Store.Audit(ctx, store.AuditEvent{UserID: user.ID, Action: "signin_link_sent"})
	link := h.PublicURL + "/signin/verify?t=" + url.QueryEscape(raw)
	h.reply(ctx, inst, info.Chat, fmt.Sprintf("Here's your %s sign-in link. It works once, for 15 minutes:\n\n%s\n\nIf you didn't ask for this, ignore this message.", h.ProductName, link))
}

func (h *Handler) reply(ctx context.Context, inst *whatsapp.DeviceInstance, to types.JID, text string) {
	if err := h.Send.SendText(ctx, inst, to, text); err != nil {
		logrus.WithError(err).Warn("[WTF] sign-in reply failed")
	}
}
```

Before running, check `types.MessageInfo` in the pinned whatsmeow for the alternate-sender field name (`SenderAlt` in current versions): `go doc go.mau.fi/whatsmeow/types MessageSource`. If it differs, use the field that holds the PN for a LID sender. The `ResolvePN` fallback covers the rest.

- [ ] **Step 4: Run the tests.** Run `cd src && go test ./multiuser/signin/ -v -race`. Expected: PASS.

- [ ] **Step 5: Break-it check.** Remove the `inst.ID() != h.ServiceDeviceID(ctx)` condition and confirm `TestOnEventOnlyServesTheServiceDevice` fails. Then revert.

- [ ] **Step 6: Commit.**

```bash
git add src/multiuser/signin
git commit -m "feat(wtf): sign-in over WhatsApp through the service number"
```

---

### Task 7: `admin-link` shell command, and keeping it from starting WhatsApp

**Files:**
- Create: `src/cmd/admin_link.go`, `src/cmd/multiuser.go` (only `loadMultiUserSettings()` in this task)
- Modify: `src/cmd/root.go` (the `OnInitialize` skip)
- Test: `src/cmd/admin_link_test.go`

**Interfaces:**
- Consumes: `multiuser.Load`, `store.Open/UserByPhone/CreateUser/CreateLoginToken/Audit`, `store.NormalizePhone`.
- Produces:
  - `cmd.skipAppInitAnnotation = "skip-app-init"`
  - `cmd.commandSkipsAppInit(args []string) bool`
  - `cmd.loadMultiUserSettings() (multiuser.Settings, error)`: reads `os.Getenv`; also used by Task 12
  - `cmd.mintAdminLink(ctx, st *store.Store, s multiuser.Settings, phoneRaw string) (link string, err error)`

- [ ] **Step 1: Write the failing tests.**

```go
// src/cmd/admin_link_test.go
package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

func TestAdminLinkSkipsAppInit(t *testing.T) {
	require.True(t, commandSkipsAppInit([]string{"admin-link", "+27825550100"}))
	require.False(t, commandSkipsAppInit([]string{"rest"}))
	require.False(t, commandSkipsAppInit([]string{}))
}

func TestMintAdminLink(t *testing.T) {
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	defer st.Close()
	s := multiuser.Settings{PublicURL: "https://wtf.onrender.com"}

	link, err := mintAdminLink(context.Background(), st, s, "+27 82 555 0100")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(link, "https://wtf.onrender.com/signin/verify?t="))
	u, err := st.UserByPhone(context.Background(), "+27825550100")
	require.NoError(t, err, "user created")

	_, err = mintAdminLink(context.Background(), st, s, "+27 82 555 0100")
	require.NoError(t, err, "second run reuses the user")
	users, _ := st.ListUsers(context.Background())
	require.Len(t, users, 1)
	_ = u

	_, err = mintAdminLink(context.Background(), st, s, "not a number")
	require.Error(t, err)
}
```

- [ ] **Step 2: Run them and watch them fail.** Run `cd src && go test ./cmd/ -run 'AdminLink'`. Expected: FAIL (undefined).

- [ ] **Step 3: Implement.**

```go
// src/cmd/multiuser.go
package cmd

import (
	"os"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
)

// loadMultiUserSettings reads WTF_* settings straight from the environment
// (Render injects them; .env is already loaded into the process by utils.LoadConfig).
func loadMultiUserSettings() (multiuser.Settings, error) {
	return multiuser.Load(os.Getenv)
}
```

Check: `utils.LoadConfig(".")` loads `.env` into Viper, not necessarily into `os.Environ`. If local `.env` WTF_* values aren't seen, change the getenv to `func(k string) string { if v := os.Getenv(k); v != "" { return v }; return viper.GetString(strings.ToLower(k)) }`.

```go
// src/cmd/admin_link.go
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/spf13/cobra"
)

const skipAppInitAnnotation = "skip-app-init"

var adminLinkCmd = &cobra.Command{
	Use:         "admin-link <phone>",
	Short:       "Print a one-time WTF sign-in link for a phone number (bootstrap and recovery)",
	Args:        cobra.ExactArgs(1),
	Annotations: map[string]string{skipAppInitAnnotation: "true"},
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := loadMultiUserSettings()
		if err != nil {
			return err
		}
		if !s.Enabled {
			return errors.New("WTF_MULTIUSER is not true")
		}
		st, err := store.Open(s.DBURI)
		if err != nil {
			return err
		}
		defer st.Close()
		link, err := mintAdminLink(cmd.Context(), st, s, args[0])
		if err != nil {
			return err
		}
		phone, _ := store.NormalizePhone(args[0])
		if !multiuser.IsSuperadmin(s, phone) {
			fmt.Fprintln(os.Stderr, "note: this number is not in WTF_SUPERADMIN_PHONES, so it signs in as an ordinary user")
		}
		fmt.Println(link)
		fmt.Fprintln(os.Stderr, "valid once, for 15 minutes")
		return nil
	},
}

func init() { rootCmd.AddCommand(adminLinkCmd) }

func mintAdminLink(ctx context.Context, st *store.Store, s multiuser.Settings, phoneRaw string) (string, error) {
	phone, err := store.NormalizePhone(phoneRaw)
	if err != nil {
		return "", err
	}
	u, err := st.UserByPhone(ctx, phone)
	if errors.Is(err, store.ErrNotFound) {
		u, err = st.CreateUser(ctx, phone)
	}
	if err != nil {
		return "", err
	}
	raw, err := st.CreateLoginToken(ctx, phone, 15*time.Minute)
	if err != nil {
		return "", err
	}
	_ = st.Audit(ctx, store.AuditEvent{UserID: u.ID, Action: "admin_link_minted"})
	return s.PublicURL + "/signin/verify?t=" + url.QueryEscape(raw), nil
}

// commandSkipsAppInit reports whether the command about to run opted out of
// initApp. initApp connects every WhatsApp session; a second process doing so
// next to the server would fight it for the same sessions.
func commandSkipsAppInit(args []string) bool {
	c, _, err := rootCmd.Find(args)
	return err == nil && c != nil && c.Annotations[skipAppInitAnnotation] == "true"
}
```

In `src/cmd/root.go` `init()`, replace `cobra.OnInitialize(initEnvConfig, initApp)` with:

```go
	cobra.OnInitialize(initEnvConfig, func() { // WTF: admin-link must not start WhatsApp
		if commandSkipsAppInit(os.Args[1:]) {
			return
		}
		initApp()
	})
```

- [ ] **Step 4: Run the tests.** Run `cd src && go test ./cmd/ -run 'AdminLink|DeviceGroupIsRegisteredLast' -v`. Expected: PASS.

- [ ] **Step 5: Manual check.**

```bash
cd src && WTF_MULTIUSER=true SESSION_SECRET=0123456789abcdef0123456789abcdef WTF_DB_URI=file:/tmp/wtf-adminlink.db go run . admin-link +27825550100
```

Expected: one `http://localhost:3000/signin/verify?t=...` line plus the two stderr notes. There must be no WhatsApp connection logs (no `Main`/`Database` lines). Delete `/tmp/wtf-adminlink.db` afterwards.

- [ ] **Step 6: Commit.**

```bash
git add src/cmd/admin_link.go src/cmd/admin_link_test.go src/cmd/multiuser.go src/cmd/root.go
git commit -m "feat(wtf): admin-link command for bootstrap and recovery, without starting WhatsApp"
```

---

### Task 8: Passkeys (registration and discoverable sign-in)

**Files:**
- Create: `src/multiuser/passkey/passkey.go`
- Test: `src/multiuser/passkey/passkey_test.go`
- Modify: `src/go.mod`, `src/go.sum` (`go get github.com/go-webauthn/webauthn@latest`, then `go mod tidy`)

**Interfaces:**
- Consumes: `store.Passkey*`, `store.UserByID`, `store.StatusActive`.
- Produces:
  - `passkey.New(rpID, origin, displayName string, st *store.Store) (*Service, error)`
  - `(*Service).BeginRegistration(ctx, u *store.User) (optionsJSON []byte, ceremonyID string, err error)`
  - `(*Service).FinishRegistration(ctx, u *store.User, ceremonyID string, body []byte, name string) error`
  - `(*Service).BeginLogin(ctx) (optionsJSON []byte, ceremonyID string, err error)`
  - `(*Service).FinishLogin(ctx, ceremonyID string, body []byte) (*store.User, error)`: refuses disabled users
  - `passkey.ErrCeremony`: unknown, expired or someone else's ceremony
  - Ceremonies live in memory for 5 minutes, keyed by a random id that the web layer keeps in an `HttpOnly` cookie `wtf_pk`

- [ ] **Step 1: Add the dependency.** Run `cd src && go get github.com/go-webauthn/webauthn@latest && go mod tidy`. Record the resolved version in the commit message. Then check the pinned API names this task uses: `go doc github.com/go-webauthn/webauthn/webauthn WebAuthn.BeginRegistration`, `WebAuthn.CreateCredential`, `WebAuthn.BeginDiscoverableLogin`, `WebAuthn.ValidatePasskeyLogin`, `go doc github.com/go-webauthn/webauthn/protocol ParseCredentialCreationResponseBytes` and `ParseCredentialRequestResponseBytes`. If a name differs in the pinned version, use the equivalent documented there.

- [ ] **Step 2: Write the failing tests.** A full ceremony needs a real authenticator, so the unit tests cover everything around it. The real ceremony is covered by the manual check in Task 11 and by `walk-user-paths` in Task 14.

```go
// src/multiuser/passkey/passkey_test.go
package passkey

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

func newSvc(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	s, err := New("wtf.onrender.com", "https://wtf.onrender.com", "WTF", st)
	require.NoError(t, err)
	return s, st
}

func TestBeginRegistrationAsksForADiscoverableCredential(t *testing.T) {
	s, st := newSvc(t)
	u, _ := st.CreateUser(context.Background(), "+27825550100")
	opts, id, err := s.BeginRegistration(context.Background(), u)
	require.NoError(t, err)
	require.NotEmpty(t, id)
	var parsed struct {
		PublicKey struct {
			RP                     struct{ ID string } `json:"rp"`
			AuthenticatorSelection struct {
				ResidentKey string `json:"residentKey"`
			} `json:"authenticatorSelection"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(opts, &parsed))
	require.Equal(t, "wtf.onrender.com", parsed.PublicKey.RP.ID)
	require.Equal(t, "required", parsed.PublicKey.AuthenticatorSelection.ResidentKey, "usernameless sign-in needs a resident key")
}

func TestCeremoniesAreSingleUseUserBoundAndExpire(t *testing.T) {
	s, st := newSvc(t)
	a, _ := st.CreateUser(context.Background(), "+27825550100")
	b, _ := st.CreateUser(context.Background(), "+27825550101")
	_, id, err := s.BeginRegistration(context.Background(), a)
	require.NoError(t, err)

	err = s.FinishRegistration(context.Background(), b, id, []byte(`{}`), "x")
	require.ErrorIs(t, err, ErrCeremony, "b cannot finish a's ceremony")
	err = s.FinishRegistration(context.Background(), a, id, []byte(`{}`), "x")
	require.ErrorIs(t, err, ErrCeremony, "a ceremony is consumed by its first attempt")

	_, id2, _ := s.BeginLogin(context.Background())
	s.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
	_, err = s.FinishLogin(context.Background(), id2, []byte(`{}`))
	require.ErrorIs(t, err, ErrCeremony, "expired")
}
```

- [ ] **Step 3: Run them and watch them fail.** Run `cd src && go test ./multiuser/passkey/`. Expected: FAIL (package missing).

- [ ] **Step 4: Implement.**

```go
// src/multiuser/passkey/passkey.go
// Package passkey wraps go-webauthn for WTF: discoverable (usernameless) passkeys.
package passkey

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

var ErrCeremony = errors.New("passkey request expired; please try again")

const ceremonyTTL = 5 * time.Minute

type ceremony struct {
	data    webauthn.SessionData
	userID  string // empty for login
	expires time.Time
}

type Service struct {
	wa  *webauthn.WebAuthn
	st  *store.Store
	mu  sync.Mutex
	cer map[string]ceremony
	now func() time.Time
}

func New(rpID, origin, displayName string, st *store.Store) (*Service, error) {
	wa, err := webauthn.New(&webauthn.Config{RPID: rpID, RPDisplayName: displayName, RPOrigins: []string{origin}})
	if err != nil {
		return nil, err
	}
	return &Service{wa: wa, st: st, cer: map[string]ceremony{}, now: time.Now}, nil
}

type waUser struct {
	u     *store.User
	creds []webauthn.Credential
}

func (w waUser) WebAuthnID() []byte                         { return []byte(w.u.ID) }
func (w waUser) WebAuthnName() string                       { return w.u.Phone }
func (w waUser) WebAuthnDisplayName() string                { return w.u.Phone }
func (w waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (s *Service) load(ctx context.Context, u *store.User) (waUser, error) {
	rows, err := s.st.PasskeysOf(ctx, u.ID)
	if err != nil {
		return waUser{}, err
	}
	w := waUser{u: u}
	for _, r := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal(r.CredentialJSON, &c); err == nil {
			w.creds = append(w.creds, c)
		}
	}
	return w, nil
}

func (s *Service) put(data *webauthn.SessionData, userID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, v := range s.cer {
		if now.After(v.expires) {
			delete(s.cer, k)
		}
	}
	id := store.NewToken()
	s.cer[id] = ceremony{data: *data, userID: userID, expires: now.Add(ceremonyTTL)}
	return id
}

// take removes the ceremony whatever happens next: one attempt per ceremony.
func (s *Service) take(id, userID string) (webauthn.SessionData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cer[id]
	delete(s.cer, id)
	if !ok || s.now().After(c.expires) || c.userID != userID {
		return webauthn.SessionData{}, ErrCeremony
	}
	return c.data, nil
}

func (s *Service) BeginRegistration(ctx context.Context, u *store.User) ([]byte, string, error) {
	w, err := s.load(ctx, u)
	if err != nil {
		return nil, "", err
	}
	var exclude []protocol.CredentialDescriptor
	for _, c := range w.creds {
		exclude = append(exclude, c.Descriptor())
	}
	opts, data, err := s.wa.BeginRegistration(w,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclude))
	if err != nil {
		return nil, "", err
	}
	js, err := json.Marshal(opts)
	if err != nil {
		return nil, "", err
	}
	return js, s.put(data, u.ID), nil
}

func (s *Service) FinishRegistration(ctx context.Context, u *store.User, ceremonyID string, body []byte, name string) error {
	data, err := s.take(ceremonyID, u.ID)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return err
	}
	w, err := s.load(ctx, u)
	if err != nil {
		return err
	}
	cred, err := s.wa.CreateCredential(w, data, parsed)
	if err != nil {
		return err
	}
	js, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	return s.st.AddPasskey(ctx, store.Passkey{
		CredentialID: base64.RawURLEncoding.EncodeToString(cred.ID), UserID: u.ID, Name: name, CredentialJSON: js,
	})
}

func (s *Service) BeginLogin(ctx context.Context) ([]byte, string, error) {
	opts, data, err := s.wa.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", err
	}
	js, err := json.Marshal(opts)
	if err != nil {
		return nil, "", err
	}
	return js, s.put(data, ""), nil
}

func (s *Service) FinishLogin(ctx context.Context, ceremonyID string, body []byte) (*store.User, error) {
	data, err := s.take(ceremonyID, "")
	if err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return nil, err
	}
	var found *store.User
	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		pk, err := s.st.PasskeyByCredentialID(ctx, base64.RawURLEncoding.EncodeToString(rawID))
		if err != nil || !bytes.Equal([]byte(pk.UserID), userHandle) {
			return nil, errors.New("unknown passkey")
		}
		u, err := s.st.UserByID(ctx, pk.UserID)
		if err != nil {
			return nil, err
		}
		found = u
		return s.load(ctx, u)
	}
	_, cred, err := s.wa.ValidatePasskeyLogin(handler, data, parsed)
	if err != nil {
		return nil, err
	}
	if found == nil || found.Status != store.StatusActive {
		return nil, errors.New("this account is disabled")
	}
	js, _ := json.Marshal(cred)
	_ = s.st.UpdatePasskeyCredential(ctx, base64.RawURLEncoding.EncodeToString(cred.ID), js)
	return found, nil
}
```

- [ ] **Step 5: Run the tests.** Run `cd src && go test ./multiuser/passkey/ -v`. Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add src/multiuser/passkey src/go.mod src/go.sum
git commit -m "feat(wtf): discoverable passkeys (go-webauthn <version>)"
```

---

### Task 9: Web foundation and the sign-in pages

**Files:**
- Create: `src/multiuser/web/web.go`, `src/multiuser/web/signin.go`
- Create: `src/multiuser/web/templates/layout.html`, `signin.html`, `verify.html`, `error.html`
- Create: `src/multiuser/web/static/app.css`, `src/multiuser/web/static/app.js`, `src/multiuser/web/static/htmx.min.js`
- Test: `src/multiuser/web/web_test.go`

**Interfaces:**
- Consumes: `auth.Manager` (Task 3), `passkey.Service` (Task 8), `store.*`, `signin.Phrase` (Task 6), `domainDevice.IDeviceUsecase` (the scoped one from Task 4).
- Produces:
  - `web.Deps{Store *store.Store; Auth *auth.Manager; Passkeys *passkey.Service; Devices domainDevice.IDeviceUsecase; ProductName string; ServicePhone func(ctx context.Context) string; Paired func() []owners.PairedDevice; StorageDir string}`. `ServicePhone` returns digits or `""` when no service number is set.
  - `web.Mount(app *fiber.App, d Deps)`: registers every page and fragment route with its own gates. It must be called after `d.Auth.Middleware()` is installed and before the API gate.
  - `web.ErrorHandler(d Deps) fiber.ErrorHandler`: friendly HTML for browser paths, JSON for machine paths
  - `web.IsMachinePath(path string) bool`

Routes in this task:

| Method | Path | Gate | Does |
|---|---|---|---|
| GET | `/ui/static/*` | none | embedded static files, `Cache-Control: public, max-age=3600` |
| GET | `/signin` | none (signed in: redirect to `/`) | Continue with WhatsApp (`wa.me` link plus QR) or Sign in with a passkey |
| GET | `/signin/qr.png` | none | a QR of the `wa.me` link (`go-qrcode`, already a dependency) |
| GET | `/signin/verify?t=` | none | confirm page with a POST form; **never consumes** |
| POST | `/signin/verify` | none | consumes the token, starts the session, 303 to `next` or `/` |
| POST | `/signout` | page + CSRF | ends the session, 303 to `/signin` |
| POST | `/auth/passkey/login/begin` | none | JSON options; sets the `wtf_pk` cookie |
| POST | `/auth/passkey/login/finish` | none | JSON `{ok:true}`, starts the session |

Template rules:
- `html/template` only, parsed once from `embed.FS`.
- Every page sets `Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`.
- No inline scripts or styles. htmx gets `<meta name="htmx-config" content='{"includeIndicatorStyles":false}'>` so it injects no inline style.
- `htmx.min.js` is downloaded once: `curl -fsSL https://unpkg.com/htmx.org@2.0.4/dist/htmx.min.js -o src/multiuser/web/static/htmx.min.js`. Check its sha256 against the one published for 2.0.4 and record it in a comment at the top of `web.go`.
- `next` is only honoured when it starts with `/` and not `//` (open-redirect guard).

- [ ] **Step 1: Load the UI skills.** Load `frontend-house-style` and `ui-ux-checklist`, and apply their tokens and layout rules in `app.css` and the templates. Below is the minimum markup each page must carry.

- [ ] **Step 2: Write the failing tests.**

```go
// src/multiuser/web/web_test.go
package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/passkey"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type env struct {
	app *fiber.App
	st  *store.Store
	d   Deps
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open("file:" + filepath.Join(t.TempDir(), "a.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	am := auth.New(auth.Config{Secret: []byte("0123456789abcdef0123456789abcdef"), SessionTTL: 30 * 24 * time.Hour,
		Superadmins: map[string]bool{"+27825550100": true}, Secure: true}, st)
	pk, err := passkey.New("wtf.onrender.com", "https://wtf.onrender.com", "WTF", st)
	require.NoError(t, err)
	d := Deps{Store: st, Auth: am, Passkeys: pk, ProductName: "WTF",
		ServicePhone: func(context.Context) string { return "27825550000" }, StorageDir: t.TempDir()}
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(d)})
	app.Use(am.Middleware())
	Mount(app, d)
	app.Use(am.RequireAPI()) // what cmd/rest.go installs after Mount
	return &env{app: app, st: st, d: d}
}

func (e *env) do(t *testing.T, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := e.app.Test(req)
	require.NoError(t, err)
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func TestSigninPageOffersWhatsAppAndPasskey(t *testing.T) {
	e := newEnv(t)
	resp, body := e.do(t, httptest.NewRequest(http.MethodGet, "/signin", nil))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html")
	require.Contains(t, resp.Header.Get("Content-Security-Policy"), "script-src 'self'")
	require.Contains(t, body, "https://wa.me/27825550000?text=Sign+me+in+to+WTF")
	require.Contains(t, body, "Sign in with a passkey")
	require.NotContains(t, body, "<script>", "no inline script")
}

func TestVerifyGETNeverConsumesAndPOSTSignsIn(t *testing.T) {
	e := newEnv(t)
	_, _ = e.st.CreateUser(context.Background(), "+27825550101")
	raw, _ := e.st.CreateLoginToken(context.Background(), "+27825550101", 15*time.Minute)

	for i := 0; i < 3; i++ { // link previews and scanners
		resp, body := e.do(t, httptest.NewRequest(http.MethodGet, "/signin/verify?t="+url.QueryEscape(raw), nil))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Contains(t, body, `method="post"`)
	}
	ok, _ := e.st.LoginTokenValid(context.Background(), raw)
	require.True(t, ok, "GET did not consume the token")

	form := url.Values{"t": {raw}, "next": {"//evil.example"}}
	req := httptest.NewRequest(http.MethodPost, "/signin/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	require.Equal(t, "/", resp.Header.Get("Location"), "open redirect refused")
	var gotCookie bool
	for _, c := range resp.Cookies() {
		gotCookie = gotCookie || (c.Name == auth.CookieName && c.Value != "")
	}
	require.True(t, gotCookie)

	resp, body := e.do(t, req.Clone(context.Background()))
	_ = resp
	require.Contains(t, body, "already been used or has expired", "second POST refused politely")
}

func TestBrowser404IsHTMLAndAPI401IsJSON(t *testing.T) {
	e := newEnv(t)
	resp, body := e.do(t, httptest.NewRequest(http.MethodGet, "/admin/users", nil))
	require.Equal(t, http.StatusSeeOther, resp.StatusCode) // anonymous: to sign-in
	resp, body = e.do(t, httptest.NewRequest(http.MethodGet, "/devices", nil))
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "application/json")
	_ = body
}

func TestNoServiceNumberExplainsInsteadOfBreaking(t *testing.T) {
	e := newEnv(t)
	e.d.ServicePhone = func(context.Context) string { return "" }
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	app.Use(e.d.Auth.Middleware())
	Mount(app, e.d)
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/signin", nil))
	require.NoError(t, err)
	b, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(b), "isn't set up yet")
	require.NotContains(t, string(b), "wa.me")
}

func TestTemplatesNeverUseBrowserDialogs(t *testing.T) {
	entries, err := templatesFS.ReadDir("templates")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, f := range entries {
		b, _ := templatesFS.ReadFile("templates/" + f.Name())
		for _, banned := range []string{"alert(", "confirm(", "hx-confirm", "onclick=", "style=\""} {
			require.NotContains(t, string(b), banned, f.Name())
		}
	}
	js, _ := staticFS.ReadFile("static/app.js")
	require.NotContains(t, string(js), "alert(")
	require.NotContains(t, string(js), "confirm(")
}
```

- [ ] **Step 3: Run them and watch them fail.** Run `cd src && go test ./multiuser/web/`. Expected: FAIL (package missing).

- [ ] **Step 4: Implement `web.go`.**

```go
// src/multiuser/web/web.go
// Package web serves WTF's own pages: sign-in, My WhatsApp, account and admin.
// htmx.min.js is htmx 2.0.4, sha256 <record here when vendoring>.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"html/template"
	"io/fs"
	"strings"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/passkey"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"ago": humanAgo,
}).ParseFS(templatesFS, "templates/*.html"))

const csp = "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

type Deps struct {
	Store        *store.Store
	Auth         *auth.Manager
	Passkeys     *passkey.Service
	Devices      domainDevice.IDeviceUsecase
	ProductName  string
	ServicePhone func(ctx context.Context) string
	Paired       func() []owners.PairedDevice
	StorageDir   string
}

// page is the data every template gets.
type page struct {
	Product    string
	Title      string
	User       *store.User
	Superadmin bool
	CSRF       string
	Data       any
}

func (d Deps) render(c fiber.Ctx, status int, name, title string, data any) error {
	u, _ := auth.UserFrom(c.Context())
	p := page{Product: d.ProductName, Title: title, User: u, Superadmin: d.Auth.IsSuperadmin(u), CSRF: d.Auth.CSRFToken(c), Data: data}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, p); err != nil {
		return err
	}
	c.Set(fiber.HeaderContentSecurityPolicy, csp)
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Referrer-Policy", "same-origin")
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Type("html", "utf-8")
	return c.Status(status).Send(buf.Bytes())
}

func Mount(app *fiber.App, d Deps) {
	static, _ := fs.Sub(staticFS, "static")
	app.Get("/ui/static/*", func(c fiber.Ctx) error {
		name := c.Params("*")
		b, err := fs.ReadFile(static, name)
		if err != nil {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderCacheControl, "public, max-age=3600")
		c.Type(strings.TrimPrefix(extOf(name), "."))
		return c.Send(b)
	})
	mountSignin(app, d)
	mountDevices(app, d) // Task 10
	mountAccount(app, d) // Task 11
	mountAdmin(app, d)   // Task 11
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

// machinePrefixes answer in JSON. Everything else is a page a person reads.
var machinePrefixes = []string{"/auth/passkey/", "/ui/devices/", "/app/", "/send/", "/user/", "/message/",
	"/chat/", "/chats", "/group/", "/newsletter/", "/devices", "/call/", "/schedule", "/mcp", "/health"}

func IsMachinePath(path string) bool {
	for _, p := range machinePrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func ErrorHandler(d Deps) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		msg := "Something went wrong at our end."
		var fe *fiber.Error
		if errors.As(err, &fe) {
			code = fe.Code
			msg = fe.Message
			if code == fiber.StatusNotFound {
				msg = "We couldn't find that page."
			}
		} else {
			logrus.WithError(err).Errorf("[WTF] unhandled on %s", c.Path())
		}
		if IsMachinePath(c.Path()) {
			return c.Status(code).JSON(fiber.Map{"status": code, "code": "ERROR", "message": msg})
		}
		return d.render(c, code, "error.html", "Error", map[string]any{"Message": msg, "Code": code})
	}
}

func safeNext(next string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.HasPrefix(next, "/\\") {
		return next
	}
	return "/"
}
```

`/ui/devices/` is listed as a machine path because its fragments are htmx partials. An htmx error swaps in a short JSON-free message: Task 10 returns HTML fragments explicitly, and this prefix only affects unexpected errors. Implement `humanAgo(t *time.Time) string` in `web.go`: "never", "just now", "5 minutes ago", "3 days ago".

- [ ] **Step 5: Implement `signin.go`.**

```go
// src/multiuser/web/signin.go
package web

import (
	"net/url"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/signin"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/gofiber/fiber/v3"
	qrcode "github.com/skip2/go-qrcode"
)

const pkCookie = "wtf_pk"

func (d Deps) waLink(c fiber.Ctx) string {
	phone := d.ServicePhone(c.Context())
	if phone == "" {
		return ""
	}
	return "https://wa.me/" + phone + "?" + url.Values{"text": {signin.Phrase + " " + d.ProductName}}.Encode()
}

func mountSignin(app *fiber.App, d Deps) {
	app.Get("/signin", func(c fiber.Ctx) error {
		if _, ok := userOf(c); ok {
			return c.Redirect().Status(fiber.StatusSeeOther).To("/")
		}
		return d.render(c, fiber.StatusOK, "signin.html", "Sign in", map[string]any{"WALink": d.waLink(c)})
	})
	app.Get("/signin/qr.png", func(c fiber.Ctx) error {
		link := d.waLink(c)
		if link == "" {
			return fiber.ErrNotFound
		}
		png, err := qrcode.Encode(link, qrcode.Medium, 320)
		if err != nil {
			return err
		}
		c.Type("png")
		return c.Send(png)
	})
	app.Get("/signin/verify", func(c fiber.Ctx) error {
		raw := c.Query("t")
		ok, err := d.Store.LoginTokenValid(c.Context(), raw)
		if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "verify.html", "Sign in", map[string]any{
			"Token": raw, "Valid": ok, "Next": safeNext(c.Query("next")),
		})
	})
	app.Post("/signin/verify", func(c fiber.Ctx) error {
		phone, err := d.Store.ConsumeLoginToken(c.Context(), c.FormValue("t"))
		if err != nil {
			return d.render(c, fiber.StatusOK, "verify.html", "Sign in", map[string]any{"Valid": false})
		}
		u, err := d.Store.UserByPhone(c.Context(), phone)
		if err != nil || u.Status != store.StatusActive {
			return d.render(c, fiber.StatusOK, "verify.html", "Sign in", map[string]any{"Valid": false})
		}
		if err := d.Auth.StartSession(c, u); err != nil {
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "signin", IP: c.IP()})
		return c.Redirect().Status(fiber.StatusSeeOther).To(safeNext(c.FormValue("next")))
	})
	app.Post("/signout", d.Auth.RequirePage(), d.Auth.VerifyCSRF(), func(c fiber.Ctx) error {
		if err := d.Auth.EndSession(c); err != nil {
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/signin")
	})
	app.Post("/auth/passkey/login/begin", func(c fiber.Ctx) error {
		opts, id, err := d.Passkeys.BeginLogin(c.Context())
		if err != nil {
			return err
		}
		c.Cookie(&fiber.Cookie{Name: pkCookie, Value: id, Path: "/auth/passkey/", HTTPOnly: true, Secure: true,
			SameSite: fiber.CookieSameSiteStrictMode, MaxAge: int((5 * time.Minute).Seconds())})
		c.Type("json")
		return c.Send(opts)
	})
	app.Post("/auth/passkey/login/finish", func(c fiber.Ctx) error {
		u, err := d.Passkeys.FinishLogin(c.Context(), c.Cookies(pkCookie), c.Body())
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"ok": false, "message": "That passkey didn't work. Try again or continue with WhatsApp."})
		}
		if err := d.Auth.StartSession(c, u); err != nil {
			return err
		}
		_ = d.Store.Audit(c.Context(), store.AuditEvent{UserID: u.ID, Action: "signin_passkey", IP: c.IP()})
		return c.JSON(fiber.Map{"ok": true, "next": "/"})
	})
}
```

Add a helper to `web.go`: `func userOf(c fiber.Ctx) (*store.User, bool) { return auth.UserFrom(c.Context()) }`.

The `wtf_pk` cookie uses `Secure: true`. For plain-http local dev, take it from the auth config instead: add `(*Manager).Secure() bool` in Task 3's file and use that.

- [ ] **Step 6: Templates.** Every page uses `layout.html` via `{{define "signin.html"}}{{template "head" .}}...{{template "foot" .}}{{end}}`. Minimum content:
  - `layout.html`: defines `head` and `foot`. `head` holds `<!doctype html>`, `<meta charset="utf-8">`, the viewport meta, `<title>{{.Title}} · {{.Product}}</title>`, `app.css`, the htmx config meta, `htmx.min.js` and `app.js` (both `defer`). It also holds a top bar showing the product name and, when signed in, links to My WhatsApp and Account, Admin only when `.Superadmin`, and a `<form method="post" action="/signout">` with the CSRF hidden field and a "Sign out" button.
  - `signin.html`: if `.Data.WALink` is set, show a primary button link "Continue with WhatsApp" to `.Data.WALink`, the QR `<img src="/signin/qr.png" alt="QR code to open WhatsApp">` (shown only at desktop widths via CSS), and the line "Scan with your phone, send the message, and we'll reply with a sign-in link." Otherwise show "Sign-in isn't set up yet. Please try again later." Always show a secondary button `<button type="button" data-passkey-login>Sign in with a passkey</button>` and an empty `<p data-passkey-error role="alert"></p>`.
  - `verify.html`: if `.Data.Valid`, show `<form method="post" action="/signin/verify">` with hidden `t` and `next` and a primary "Sign in" button. Otherwise show "This sign-in link has already been used or has expired. Send us the message again for a new one." with a link to `/signin`.
  - `error.html`: `.Data.Code`, `.Data.Message`, and a link home.

- [ ] **Step 7: Static assets.** `app.js`: on `[data-passkey-login]` click, POST `/auth/passkey/login/begin`, convert the base64url fields (`challenge`, and every `allowCredentials[].id`) to ArrayBuffer, call `navigator.credentials.get({publicKey})`, serialise the result (base64url `rawId`, `response.clientDataJSON`, `authenticatorData`, `signature`, `userHandle`; `type`; `id`), POST it to `/auth/passkey/login/finish` and follow `next`. On failure, put the server's message, or "Passkey sign-in was cancelled.", into `[data-passkey-error]`. Prefer `PublicKeyCredential.parseRequestOptionsFromJSON` and `credential.toJSON()` when the browser has them, and keep the manual conversion as the fallback. Add a `[data-open-dialog="<id>"]` click handler that calls `document.getElementById(id).showModal()`, and `[data-close-dialog]` to close the nearest dialog. `app.css`: the tokens and components from `frontend-house-style`, light and dark via `prefers-color-scheme`, a 44px minimum tap target, `flex-wrap: wrap` on every action group.

- [ ] **Step 8: Run the tests.** Run `cd src && go test ./multiuser/web/ -v`. Expected: PASS for the tests in this task. `mountDevices`, `mountAccount` and `mountAdmin` must exist; create them as empty functions now, and Tasks 10 and 11 fill them.

- [ ] **Step 9: Break-it check.** Make GET `/signin/verify` call `ConsumeLoginToken` and confirm `TestVerifyGETNeverConsumesAndPOSTSignsIn` fails. Then revert.

- [ ] **Step 10: Commit.**

```bash
git add src/multiuser/web
git commit -m "feat(wtf): web foundation, WhatsApp and passkey sign-in pages"
```

---

### Task 10: My WhatsApp page (link, QR, pairing code, status, reconnect, unlink)

**Files:**
- Create: `src/multiuser/web/devices.go`, `src/multiuser/web/templates/home.html`, `src/multiuser/web/templates/link_modal.html`
- Test: `src/multiuser/web/devices_test.go`

**Interfaces:**
- Consumes: `Deps.Devices` (the `owners.ScopedDevices` from Task 4): `ListDevices`, `AddDevice`, `LoginDevice`, `LoginDeviceWithCode`, `GetStatus`, `ReconnectDevice`, `RemoveDevice`.
- Produces:

| Method | Path | Gate | Returns |
|---|---|---|---|
| GET | `/` | page | home: device cards (name or number, Connected / Not connected / Not linked) and a top-right **Link WhatsApp** button |
| POST | `/ui/devices` | page + CSRF | creates a device, returns the `link_modal.html` fragment (htmx target `#link-dialog-body`) |
| GET | `/ui/devices/:id/qr.png` | page | PNG read from the `LoginDevice` result's `ImagePath`; 409 when already logged in |
| POST | `/ui/devices/:id/pair-code` | page + CSRF | fragment showing the 8-character code |
| GET | `/ui/devices/:id/status` | page | 204 while waiting; once logged in, 200 with `HX-Refresh: true` |
| POST | `/ui/devices/:id/reconnect` | page + CSRF | 303 to `/` |
| POST | `/ui/devices/:id/unlink` | page + CSRF | 303 to `/` (calls `RemoveDevice`) |

- [ ] **Step 1: Write the failing tests.** Use a fake `Devices` (an `owners.ScopedDevices` over a recording inner stub, like Task 4's `innerStub`) so ownership is exercised for real.

```go
// src/multiuser/web/devices_test.go
package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type devStub struct {
	devices  []domainDevice.Device
	qrPath   string
	loggedIn bool
}

func (s *devStub) ListDevices(context.Context) ([]domainDevice.Device, error) { return s.devices, nil }
func (s *devStub) GetDevice(_ context.Context, id string) (*domainDevice.Device, error) {
	return &domainDevice.Device{ID: id}, nil
}
func (s *devStub) AddDevice(_ context.Context, id string, _ *chatstorage.DeviceWebhookConfig) (*domainDevice.Device, error) {
	s.devices = append(s.devices, domainDevice.Device{ID: id})
	return &domainDevice.Device{ID: id}, nil
}
func (s *devStub) RemoveDevice(context.Context, string) error { return nil }
func (s *devStub) LoginDevice(context.Context, string) (domainApp.LoginResponse, error) {
	return domainApp.LoginResponse{ImagePath: s.qrPath, Duration: 20}, nil
}
func (s *devStub) LoginDeviceWithCode(context.Context, string, string) (string, error) { return "ABCD-EFGH", nil }
func (s *devStub) LogoutDevice(context.Context, string) error                           { return nil }
func (s *devStub) ReconnectDevice(context.Context, string) error                        { return nil }
func (s *devStub) GetStatus(context.Context, string) (bool, bool, error)                { return s.loggedIn, s.loggedIn, nil }
func (s *devStub) SetDeviceWebhook(context.Context, string, string) error               { return nil }
func (s *devStub) GetDeviceWebhook(context.Context, string) (string, error)             { return "", nil }
func (s *devStub) SetDeviceWebhookConfig(context.Context, string, *chatstorage.DeviceWebhookConfig) error {
	return nil
}
func (s *devStub) GetDeviceWebhookConfig(context.Context, string) (*chatstorage.DeviceWebhookConfig, error) {
	return nil, nil
}

func signedIn(t *testing.T, e *env, phone string) (*http.Cookie, string) {
	t.Helper()
	u, err := e.st.CreateUser(context.Background(), phone)
	require.NoError(t, err)
	raw, _ := e.st.CreateLoginToken(context.Background(), phone, 15*60e9)
	form := url.Values{"t": {raw}}
	req := httptest.NewRequest(http.MethodPost, "/signin/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := e.do(t, req)
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieName {
			return c, u.ID
		}
	}
	t.Fatal("no cookie")
	return nil, ""
}

func withDevices(t *testing.T, stub *devStub) *env {
	e := newEnv(t)
	e.d.Devices = owners.NewScopedDevices(stub, e.st)
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(e.d)})
	app.Use(e.d.Auth.Middleware())
	Mount(app, e.d)
	app.Use(e.d.Auth.RequireAPI())
	e.app = app
	return e
}

func TestHomeListsOnlyMyDevicesAndQRIsOwnerChecked(t *testing.T) {
	qr := filepath.Join(t.TempDir(), "scan-qr.png")
	require.NoError(t, os.WriteFile(qr, []byte("\x89PNG fake"), 0o600))
	stub := &devStub{devices: []domainDevice.Device{{ID: "d_mine"}, {ID: "d_theirs"}}, qrPath: qr}
	e := withDevices(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	_, them := signedIn(t, e, "+27825550102")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	require.NoError(t, e.st.SetOwner(context.Background(), "d_theirs", them))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(ck)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, body, "d_mine")
	require.NotContains(t, body, "d_theirs")
	require.Contains(t, body, "Link WhatsApp")

	req = httptest.NewRequest(http.MethodGet, "/ui/devices/d_theirs/qr.png", nil)
	req.AddCookie(ck)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	req = httptest.NewRequest(http.MethodGet, "/ui/devices/d_mine/qr.png", nil)
	req.AddCookie(ck)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "image/png", resp.Header.Get("Content-Type"))
}

func TestStatusRefreshesWhenLinked(t *testing.T) {
	stub := &devStub{}
	e := withDevices(t, stub)
	ck, me := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_mine", me))
	req := httptest.NewRequest(http.MethodGet, "/ui/devices/d_mine/status", nil)
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	stub.loggedIn = true
	resp, _ = e.do(t, req.Clone(context.Background()))
	require.Equal(t, "true", resp.Header.Get("HX-Refresh"))
}

func TestCreateDeviceNeedsCSRF(t *testing.T) {
	e := withDevices(t, &devStub{})
	ck, _ := signedIn(t, e, "+27825550101")
	req := httptest.NewRequest(http.MethodPost, "/ui/devices", nil)
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}
```

- [ ] **Step 2: Run them and watch them fail.** Run `cd src && go test ./multiuser/web/ -run 'Home|Status|CreateDevice'`. Expected: FAIL.

- [ ] **Step 3: Implement `devices.go`.**

```go
// src/multiuser/web/devices.go
package web

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/gofiber/fiber/v3"
)

func mountDevices(app *fiber.App, d Deps) {
	page := d.Auth.RequirePage()
	csrf := d.Auth.VerifyCSRF()

	app.Get("/", page, func(c fiber.Ctx) error {
		list, err := d.Devices.ListDevices(c.Context())
		if err != nil {
			return err
		}
		return d.render(c, fiber.StatusOK, "home.html", "My WhatsApp", map[string]any{"Devices": list})
	})

	app.Post("/ui/devices", page, csrf, func(c fiber.Ctx) error {
		dev, err := d.Devices.AddDevice(c.Context(), "", nil)
		if err != nil {
			var v pkgError.ValidationError
			if errors.As(err, &v) {
				return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"Error": v.Error()})
			}
			return err
		}
		return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"ID": dev.ID})
	})

	app.Get("/ui/devices/:id/qr.png", page, func(c fiber.Ctx) error {
		resp, err := d.Devices.LoginDevice(c.Context(), c.Params("id"))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if errors.Is(err, pkgError.ErrAlreadyLoggedIn) {
			return c.SendStatus(fiber.StatusConflict)
		}
		if err != nil {
			return err
		}
		path := filepath.Clean(resp.ImagePath)
		if strings.Contains(path, "..") {
			return fiber.ErrNotFound
		}
		png, err := os.ReadFile(path)
		if err != nil {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderCacheControl, "no-store")
		c.Type("png")
		return c.Send(png)
	})

	app.Post("/ui/devices/:id/pair-code", page, csrf, func(c fiber.Ctx) error {
		code, err := d.Devices.LoginDeviceWithCode(c.Context(), c.Params("id"), c.FormValue("phone"))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"ID": c.Params("id"), "Error": "We couldn't get a code for that number. Check it and try again."})
		}
		return d.render(c, fiber.StatusOK, "link_modal.html", "", map[string]any{"ID": c.Params("id"), "PairCode": code})
	})

	app.Get("/ui/devices/:id/status", page, func(c fiber.Ctx) error {
		_, loggedIn, err := d.Devices.GetStatus(c.Context(), c.Params("id"))
		if errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil || !loggedIn {
			return c.SendStatus(fiber.StatusNoContent)
		}
		c.Set("HX-Refresh", "true")
		return c.SendStatus(fiber.StatusOK)
	})

	app.Post("/ui/devices/:id/reconnect", page, csrf, func(c fiber.Ctx) error {
		if err := d.Devices.ReconnectDevice(c.Context(), c.Params("id")); errors.Is(err, pkgError.ErrDeviceNotFound) {
			return fiber.ErrNotFound
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/")
	})

	app.Post("/ui/devices/:id/unlink", page, csrf, func(c fiber.Ctx) error {
		if err := d.Devices.RemoveDevice(c.Context(), c.Params("id")); err != nil {
			if errors.Is(err, pkgError.ErrDeviceNotFound) {
				return fiber.ErrNotFound
			}
			return err
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/")
	})
}
```

`ScopedDevices` returns `pkgError.ErrDeviceNotFound` for someone else's device. Confirm `errors.Is` matches it: it's a comparable string-typed value, so `==` comparison works.

- [ ] **Step 4: Templates.**
  - `home.html`: a page head with the `h1` "My WhatsApp" and, top right, `<button type="button" data-open-dialog="link-dialog" hx-post="/ui/devices" hx-target="#link-dialog-body" hx-headers='{"X-CSRF-Token":"{{.CSRF}}"}'>Link WhatsApp</button>`.
    - Under it, one card per device: phone number (or "Not linked yet"), state, and actions. Reconnect shows only when the device is linked but disconnected. Unlink opens a confirm `<dialog>` holding a POST form. An unlinked device shows "Link" (reopens the modal for that id via `hx-get` on a `link_modal` render) and "Remove".
    - When there are no devices, show an empty state: "Link your WhatsApp to get started."
    - Then `<dialog id="link-dialog"><div id="link-dialog-body"></div><button type="button" data-close-dialog>Close</button></dialog>`.
  - `link_modal.html` (a fragment with no `head`/`foot`):
    - If `.Data.Error`, show it.
    - Else, if `.Data.PairCode`, show "On your phone: WhatsApp, Linked devices, Link with phone number, then enter" and the code in large type.
    - Else show three things:
      - `<img src="/ui/devices/{{.Data.ID}}/qr.png" alt="QR code to link WhatsApp" hx-get="/ui/devices/{{.Data.ID}}/qr.png" hx-trigger="every 20s" hx-swap="none">`. Refresh the image by setting `src` with a cache-buster from `app.js` on the `htmx:afterRequest` event for that element, since the QR rotates.
      - The steps "Open WhatsApp, Settings, Linked devices, Link a device, then scan".
      - A collapsible "Use a phone number instead" form that `hx-post`s to `/ui/devices/{{.Data.ID}}/pair-code` with the CSRF header, swapping `#link-dialog-body`.
    - Always include `<div hx-get="/ui/devices/{{.Data.ID}}/status" hx-trigger="every 3s" hx-swap="none"></div>`, so the page reloads once linked.

- [ ] **Step 5: Run the tests.** Run `cd src && go test ./multiuser/web/ -v`. Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add src/multiuser/web
git commit -m "feat(wtf): My WhatsApp page with QR and pairing-code linking"
```

---

### Task 11: Account page (passkeys) and the admin pages

**Files:**
- Create: `src/multiuser/web/account.go`, `src/multiuser/web/admin.go`
- Create: `src/multiuser/web/templates/account.html`, `admin_users.html`, `admin_settings.html`
- Test: `src/multiuser/web/admin_test.go`

**Interfaces:**
- Consumes: `store.PasskeysOf/DeletePasskey/DeleteUserSessions/ListUsers/SetUserStatus/DevicesOf/CreateLoginToken/SetSetting/Setting/OwnedDeviceCount/Audit`, `passkey.Service.BeginRegistration/FinishRegistration`, `Deps.Paired`.
- Produces:

| Method | Path | Gate | Does |
|---|---|---|---|
| GET | `/account` | page | phone number, passkey list (name, added, last used, Remove), **Add a passkey** (top right), Sign out everywhere |
| POST | `/auth/passkey/register/begin` | page + CSRF | JSON options; sets `wtf_pk` |
| POST | `/auth/passkey/register/finish` | page + CSRF | JSON `{ok}`; passkey name from the User-Agent, e.g. "Safari on iOS" |
| POST | `/account/passkeys/:cred/delete` | page + CSRF | 303 to `/account` |
| POST | `/account/signout-everywhere` | page + CSRF | deletes all sessions, 303 to `/signin` |
| GET | `/admin/users` | page + superadmin | users table: phone, status, linked phones, last sign-in, actions |
| POST | `/admin/users/:id/status` | page + superadmin + CSRF | form `status=active\|disabled`; disabling also deletes their sessions; a superadmin can't disable themselves |
| POST | `/admin/users/:id/signin-link` | page + superadmin + CSRF | renders a modal fragment with a fresh 15-minute link to copy |
| GET | `/admin/settings` | page + superadmin | service number picker (the superadmin's own linked devices), memory (`runtime.MemStats` `Alloc`/`Sys` in MB, goroutines), disk used under `StorageDir`, linked phones total |
| POST | `/admin/settings/service-number` | page + superadmin + CSRF | form `device_id` must be one of the caller's own linked devices; stored as `store.SettingServiceDeviceID`; audited |

- [ ] **Step 1: Write the failing tests.**

```go
// src/multiuser/web/admin_test.go
package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/stretchr/testify/require"
)

func csrfFor(t *testing.T, e *env, ck *http.Cookie) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	req.AddCookie(ck)
	_, body := e.do(t, req)
	i := strings.Index(body, `name="csrf" value="`)
	require.Greater(t, i, 0, "account page carries a CSRF field")
	rest := body[i+len(`name="csrf" value="`):]
	return rest[:strings.IndexByte(rest, '"')]
}

func postForm(t *testing.T, e *env, ck *http.Cookie, path string, form url.Values) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(ck)
	resp, _ := e.do(t, req)
	return resp
}

func TestAdminPagesAreSuperadminOnly(t *testing.T) {
	e := withDevices(t, &devStub{})
	plain, _ := signedIn(t, e, "+27825550101")
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req.AddCookie(plain)
	resp, body := e.do(t, req)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Contains(t, resp.Header.Get("Content-Type"), "text/html", "friendly page, not JSON")
	_ = body
}

func TestDisablingAUserEndsTheirSessions(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	victim, victimID := signedIn(t, e, "+27825550101")
	tok := csrfFor(t, e, admin)

	resp := postForm(t, e, admin, "/admin/users/"+victimID+"/status", url.Values{"csrf": {tok}, "status": {"disabled"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(victim)
	resp, _ = e.do(t, req)
	require.Equal(t, http.StatusSeeOther, resp.StatusCode, "disabled user bounced to sign-in")
	u, _ := e.st.UserByID(context.Background(), victimID)
	require.Equal(t, store.StatusDisabled, u.Status)
}

func TestSuperadminCannotDisableThemselves(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, adminID := signedIn(t, e, "+27825550100")
	tok := csrfFor(t, e, admin)
	resp := postForm(t, e, admin, "/admin/users/"+adminID+"/status", url.Values{"csrf": {tok}, "status": {"disabled"}})
	require.NotEqual(t, http.StatusSeeOther, resp.StatusCode)
	u, _ := e.st.UserByID(context.Background(), adminID)
	require.Equal(t, store.StatusActive, u.Status)
}

func TestServiceNumberMustBeTheSuperadminsOwnDevice(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, adminID := signedIn(t, e, "+27825550100")
	_, otherID := signedIn(t, e, "+27825550101")
	require.NoError(t, e.st.SetOwner(context.Background(), "d_admin", adminID))
	require.NoError(t, e.st.SetOwner(context.Background(), "d_other", otherID))
	tok := csrfFor(t, e, admin)

	resp := postForm(t, e, admin, "/admin/settings/service-number", url.Values{"csrf": {tok}, "device_id": {"d_other"}})
	require.NotEqual(t, http.StatusSeeOther, resp.StatusCode)
	_, ok, _ := e.st.Setting(context.Background(), store.SettingServiceDeviceID)
	require.False(t, ok)

	resp = postForm(t, e, admin, "/admin/settings/service-number", url.Values{"csrf": {tok}, "device_id": {"d_admin"}})
	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	v, _, _ := e.st.Setting(context.Background(), store.SettingServiceDeviceID)
	require.Equal(t, "d_admin", v)
}

func TestAdminSigninLinkIsShownNotSent(t *testing.T) {
	e := withDevices(t, &devStub{})
	admin, _ := signedIn(t, e, "+27825550100")
	_, otherID := signedIn(t, e, "+27825550101")
	tok := csrfFor(t, e, admin)
	resp := postForm(t, e, admin, "/admin/users/"+otherID+"/signin-link", url.Values{"csrf": {tok}})
	b, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(b), "/signin/verify?t=")
}
```

- [ ] **Step 2: Run them and watch them fail.** Run `cd src && go test ./multiuser/web/ -run 'Admin|Disabling|Superadmin|ServiceNumber'`. Expected: FAIL.

- [ ] **Step 3: Implement `account.go` and `admin.go`.** Follow the route table above, using the `Deps.render` and gate patterns from Tasks 9 and 10. Specifics:
  - Passkey name from the User-Agent: write `deviceName(ua string) string`, which returns "Edge on macOS", "Safari on iOS", "Chrome on Android", "Firefox on Windows", or "This device" as the fallback. Check `Edg/` before `Chrome/`, since Edge's UA contains both. Add a table test for it in `admin_test.go` with four real UA strings.
  - The status change parses `status`. If the target is the caller, render the users page with "You can't disable your own account." and a 400. On `disabled`, call `SetUserStatus`, then `DeleteUserSessions`, then `Audit{Action:"user_disabled"}`.
  - The sign-in link handler calls `CreateLoginToken(target.Phone, 15m)` and renders a fragment with a read-only `<input>` holding the link, a Copy button (`app.js`: `navigator.clipboard.writeText`), and "Valid once, for 15 minutes. Send it to them yourself."
  - The service number handler does `ids := DevicesOf(caller.ID)`; if the form `device_id` isn't in `ids`, render the settings page with "Pick one of your own linked numbers." and a 400. Otherwise call `SetSetting(SettingServiceDeviceID, id, caller.ID)` and audit it.
  - Settings page data: `runtime.ReadMemStats`, `runtime.NumGoroutine()`, a `filepath.WalkDir` total of `StorageDir`, `OwnedDeviceCount`, the caller's devices from `d.Devices.ListDevices` (it's scoped to the caller already), and the current service device id.

- [ ] **Step 4: Templates.** Keep house style: tables have a count line, an empty state, and actions in a modal when they're destructive (Disable, Remove passkey). `account.html` must carry `<input type="hidden" name="csrf" value="{{.CSRF}}">` in its forms; the test reads it.

- [ ] **Step 5: Manual passkey check.** You need a browser for this. Run the app locally per Task 12 Step 5, sign in with an `admin-link`, add a passkey on `/account`, sign out, then sign in with the passkey. Expected: you're back on `/`. Record the browser and OS in the commit message.

- [ ] **Step 6: Run the tests.** Run `cd src && go test ./multiuser/... -v`. Expected: PASS.

- [ ] **Step 7: Commit.**

```bash
git add src/multiuser/web
git commit -m "feat(wtf): account page with passkeys; admin users and settings (service number)"
```

---

### Task 12: Wire multi-user mode into `cmd/rest.go`

**Files:**
- Modify: `src/cmd/multiuser.go` (add `setupMultiUser`)
- Create: `src/multiuser/runtime.go`
- Modify: `src/cmd/rest.go`
- Test: `src/cmd/rest_route_order_test.go` must stay green; `src/cmd/multiuser_isolation_test.go` comes in Task 13

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `multiuser.Runtime{Settings Settings; Store *store.Store; Auth *auth.Manager; Resolver *owners.Resolver; Devices domainDevice.IDeviceUsecase; Signin *signin.Handler; Web web.Deps}`
  - `multiuser.Setup(s Settings, dm *whatsapp.DeviceManager, inner domainDevice.IDeviceUsecase, storageDir string) (*Runtime, error)`: opens the store, builds everything, and sets `middleware.DeviceResolveFunc` and `whatsapp.EventHook`
  - `(*Runtime).Install(app *fiber.App)`: `app.Use(Auth.Middleware())`, `web.Mount(app, Web)`, then `app.Use(Auth.RequireAPI(), Auth.VerifyCSRF())`
  - `(*Runtime).Close() error`
  - `cmd.registerDeviceScopedRoutes(r fiber.Router)`: the former closure, now top-level

- [ ] **Step 1: Implement `runtime.go`.**

```go
// src/multiuser/runtime.go
package multiuser

import (
	"context"
	"net/url"
	"strings"
	"time"

	domainDevice "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/device"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/owners"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/passkey"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/signin"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/web"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/gofiber/fiber/v3"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types/events"
)

type Runtime struct {
	Settings Settings
	Store    *store.Store
	Auth     *auth.Manager
	Resolver *owners.Resolver
	Devices  domainDevice.IDeviceUsecase
	Signin   *signin.Handler
	Web      web.Deps
}

type dmPairs struct{ dm *whatsapp.DeviceManager }

func (p dmPairs) Paired() []owners.PairedDevice {
	var out []owners.PairedDevice
	for _, inst := range p.dm.ListDevices() {
		out = append(out, owners.PairedDevice{ID: inst.ID(), JID: inst.JID()})
	}
	return out
}
func (p dmPairs) Logout(ctx context.Context, id string) error { return p.dm.LogoutDeviceKeepSlot(ctx, id) }

func Setup(s Settings, dm *whatsapp.DeviceManager, inner domainDevice.IDeviceUsecase, storageDir string) (*Runtime, error) {
	st, err := store.Open(s.DBURI)
	if err != nil {
		return nil, err
	}
	pub, err := url.Parse(s.PublicURL)
	if err != nil {
		return nil, err
	}
	secure := pub.Scheme == "https"
	am := auth.New(auth.Config{Secret: s.SessionSecret, Superadmins: s.Superadmins, SessionTTL: 30 * 24 * time.Hour, Secure: secure}, st)
	pk, err := passkey.New(pub.Hostname(), s.PublicURL, s.ProductName, st)
	if err != nil {
		return nil, err
	}
	resolver := owners.NewResolver(dm, st)
	devices := owners.NewScopedDevices(inner, st)

	serviceID := func(ctx context.Context) string { v, _, _ := st.Setting(ctx, store.SettingServiceDeviceID); return v }
	servicePhone := func(ctx context.Context) string {
		if inst, ok := dm.GetDevice(serviceID(ctx)); ok && inst.IsLoggedIn() {
			return strings.TrimPrefix(inst.PhoneNumber(), "+")
		}
		return ""
	}
	sh := &signin.Handler{
		Store: st, Send: signin.ClientSender{}, ProductName: s.ProductName, PublicURL: s.PublicURL,
		SignupOpen: s.SignupOpen, IsSuperadmin: func(p string) bool { return IsSuperadmin(s, p) },
		Limiter: signin.NewLimiter(3, time.Hour), ResolvePN: signin.DefaultResolvePN,
		ServiceDeviceID: serviceID, Now: time.Now,
	}
	pairs := dmPairs{dm: dm}

	middleware.DeviceResolveFunc = func(c fiber.Ctx, id string) (*whatsapp.DeviceInstance, string, error) {
		return resolver.Resolve(c.Context(), id)
	}
	whatsapp.EventHook = func(ctx context.Context, inst *whatsapp.DeviceInstance, evt any) {
		sh.OnEvent(ctx, inst, evt)
		if _, ok := evt.(*events.PairSuccess); ok && inst != nil {
			go func(id string) {
				time.Sleep(2 * time.Second) // let the store JID settle
				if refused, err := owners.GuardDuplicatePhone(context.Background(), pairs, st, id); refused {
					logrus.Warnf("[WTF] refused duplicate link on %s", id)
				} else if err != nil {
					logrus.WithError(err).Warn("[WTF] duplicate guard failed")
				}
			}(inst.ID())
		}
	}

	return &Runtime{Settings: s, Store: st, Auth: am, Resolver: resolver, Devices: devices, Signin: sh,
		Web: web.Deps{Store: st, Auth: am, Passkeys: pk, Devices: devices, ProductName: s.ProductName,
			ServicePhone: servicePhone, Paired: pairs.Paired, StorageDir: storageDir}}, nil
}

func (r *Runtime) Install(app *fiber.App) {
	app.Use(r.Auth.Middleware())
	web.Mount(app, r.Web)
	app.Use(r.Auth.RequireAPI(), r.Auth.VerifyCSRF())
}

func (r *Runtime) Close() error {
	middleware.DeviceResolveFunc = nil
	whatsapp.EventHook = nil
	return r.Store.Close()
}
```

Check `inst.PhoneNumber()` (device_instance.go:89) for its exact format. It may already be digits; `TrimPrefix` is harmless either way. Also check that `DeviceManager.LogoutDeviceKeepSlot` is exported (device_manager.go:419, it is).

- [ ] **Step 2: `setupMultiUser` in `cmd/multiuser.go`.**

```go
// setupMultiUser is called by restServer when WTF_MULTIUSER=true. It must run
// before any apiGroup registration so the API gate precedes upstream routes.
func setupMultiUser(app *fiber.App, dm *whatsapp.DeviceManager, s multiuser.Settings) (*multiuser.Runtime, error) {
	// multiuser.Apply() already ran at the top of restServer (Step 3.2).
	rt, err := multiuser.Setup(s, dm, deviceUsecase, config.PathStorages)
	if err != nil {
		return nil, err
	}
	deviceUsecase = rt.Devices // /devices REST routes now see only the caller's devices
	rt.Install(app)
	return rt, nil
}
```

Import `config`, `whatsapp`, `multiuser` and `fiber`.

- [ ] **Step 3: Edit `cmd/rest.go`.** Keep every change marked `// WTF:` and keep `TestDeviceGroupIsRegisteredLast` green.
  1. Move the `registerDeviceScopedRoutes` closure to a top-level `func registerDeviceScopedRoutes(r fiber.Router)` with the same body, but replace the `websocket.RegisterRoutes(r, appUsecase)` line with:

     ```go
     	if !config.MultiUserEnabled { // WTF: upstream's /ws broadcasts every user's events
     		websocket.RegisterRoutes(r, appUsecase)
     	}
     ```

     Add `MultiUserEnabled bool` to `src/config/settings.go` with the comment `// WTF: set by restServer when WTF_MULTIUSER=true`.
  2. At the top of `restServer`, before `fiber.New`, read the settings once:

     ```go
     	muSettings, err := loadMultiUserSettings() // WTF
     	if err != nil {
     		logrus.Fatalln("WTF multi-user settings: ", err.Error())
     	}
     	config.MultiUserEnabled = muSettings.Enabled
     	if config.MultiUserEnabled {
     		multiuser.Apply() // before anything reads the flags it turns off
     	}
     ```

     The existing `err` variable is declared later with `:=`, so adjust that later declaration to `=` if the compiler complains.
  3. In multi-user mode, pass `fiber.Config{ErrorHandler: ...}`. This needs the runtime, which is built after `fiber.New`, so instead set `app.Config()`'s handler through a small indirection: `var muErr fiber.ErrorHandler`, and in `fiberConfig`: `ErrorHandler: func(c fiber.Ctx, err error) error { if muErr != nil { return muErr(c, err) }; return fiber.DefaultErrorHandler(c, err) }`.
  4. Wrap the static mount: `if !config.MultiUserEnabled { app.Use(config.AppBasePath+"/statics", static.New("./statics")) } // WTF: media and QR files are owner-checked in multi-user mode`.
  5. Right after `/health` and before the Chatwoot block, add:

     ```go
     	var muRuntime *multiuser.Runtime // WTF
     	if config.MultiUserEnabled {
     		muRuntime, err = setupMultiUser(app, dm, muSettings)
     		if err != nil {
     			logrus.Fatalln("WTF multi-user setup: ", err.Error())
     		}
     		muErr = web.ErrorHandler(muRuntime.Web)
     		defer muRuntime.Close()
     	}
     ```

     Basic Auth is already skipped because `Apply()` cleared the credentials. The OAuth MCP registration is a no-op because MCP is off.
  6. Wrap `registerUIRoute(apiGroup, uiCtx)` as `if !config.MultiUserEnabled { registerUIRoute(apiGroup, uiCtx) }`. Our `/` page is registered by `web.Mount`. Check that `TestDeviceGroupIsRegisteredLast` still parses this: the call stays inside `restServer` and before the group.
  7. Replace the closure call `registerDeviceScopedRoutes(headerDeviceGroup)` (the name is unchanged, so this line stays as is).

- [ ] **Step 4: Run the tests.** Run `cd src && go build ./... && go vet ./... && go test ./cmd/ ./multiuser/...`. Expected: PASS.

- [ ] **Step 5: Local smoke run.** This uses no real WhatsApp session: a fresh storage dir has no linked devices.

```bash
cd src && mkdir -p /tmp/wtf-smoke && cd /tmp/wtf-smoke && \
WTF_MULTIUSER=true SESSION_SECRET=0123456789abcdef0123456789abcdef \
WTF_SUPERADMIN_PHONES=+27825550100 WTF_PUBLIC_URL=http://localhost:3000 \
go run /Users/aryeh.lewis/PycharmProjects/go-whatsapp-web-multidevice/src rest
```

Running from `/tmp/wtf-smoke` keeps `storages/` and `statics/` out of the repo. In another shell:
- `curl -i localhost:3000/` gives a 303 to `/signin`.
- `curl -i localhost:3000/devices` gives a 401 JSON.
- `curl -i localhost:3000/statics/` gives a 401 JSON (not served).
- `curl -i localhost:3000/ws` gives a 401.
- Run `WTF_MULTIUSER=true SESSION_SECRET=... go run <src> admin-link +27825550100` from the same dir, open the link in a browser, click Sign in, and land on My WhatsApp with an empty state.
- Do not link a real phone in this step.

Stop the server and delete `/tmp/wtf-smoke`.

- [ ] **Step 6: Commit.**

```bash
git add src/cmd src/config/settings.go src/multiuser/runtime.go
git commit -m "feat(wtf): wire multi-user mode into the REST server"
```

---

### Task 13: Cross-user isolation test over every registered route

**Files:**
- Create: `src/cmd/multiuser_isolation_test.go`

**Interfaces:**
- Consumes: `registerDeviceScopedRoutes`, `rest.InitRestDevice`, `rest.InitRestAppInfo`, `multiuser.Setup`, `whatsapp.NewDeviceManager`.

- [ ] **Step 1: Write the test.** It builds the multi-user app the way `restServer` does, with real middleware and real routes but nil usecases. Every request must be stopped by the auth gate or the owner check before any handler runs. A request that reaches a nil usecase panics, Recovery turns that into a 500, and the test fails.

```go
// src/cmd/multiuser_isolation_test.go
package cmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/multiuser/auth"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/ui/rest/middleware"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/usecase"
	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// publicRoutes may be reached signed out. Anything else that answers an
// anonymous request with something other than 303/401 fails this test.
var publicRoutes = map[string]bool{
	"GET /health": true, "GET /signin": true, "GET /signin/qr.png": true, "GET /signin/verify": true,
	"POST /signin/verify": true, "GET /ui/static/*": true,
	"POST /auth/passkey/login/begin": true, "POST /auth/passkey/login/finish": true,
	"GET /app/info": true, // check rest.InitRestAppInfo's path; it has no device data
}

func TestEveryRouteIsGatedAndOwnerScoped(t *testing.T) {
	savedMU := config.MultiUserEnabled
	t.Cleanup(func() { config.MultiUserEnabled = savedMU })
	config.MultiUserEnabled = true

	dm := whatsapp.NewDeviceManager(nil, nil, nil)
	for _, id := range []string{"d_alice", "d_bob"} {
		_, err := dm.CreateDevice(context.Background(), id)
		require.NoError(t, err)
	}
	s := multiuser.Settings{Enabled: true, DBURI: "file:" + filepath.Join(t.TempDir(), "a.db"), ProductName: "WTF",
		PublicURL: "https://wtf.test", SessionSecret: []byte("0123456789abcdef0123456789abcdef"), Superadmins: map[string]bool{}}
	rt, err := multiuser.Setup(s, dm, usecase.NewDeviceService(dm, nil), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = rt.Close() })

	app := fiber.New()
	app.Use(middleware.Recovery())
	rt.Install(app)
	rest.InitRestDevice(app, rt.Devices)
	rest.InitRestAppInfo(app)
	registerDeviceScopedRoutes(app.Group("", middleware.DeviceMiddleware(dm)))

	ctx := context.Background()
	alice, _ := rt.Store.CreateUser(ctx, "+27825550101")
	bob, _ := rt.Store.CreateUser(ctx, "+27825550102")
	require.NoError(t, rt.Store.SetOwner(ctx, "d_alice", alice.ID))
	require.NoError(t, rt.Store.SetOwner(ctx, "d_bob", bob.ID))
	rawAlice, _ := rt.Store.CreateSession(ctx, alice.ID, "", time.Hour)
	aliceCookie := &http.Cookie{Name: auth.CookieName, Value: rawAlice}
	csrf := csrfForRaw(rt, rawAlice)

	var checked int
	for _, r := range app.GetRoutes(true) {
		if r.Method == fiber.MethodHead || r.Method == fiber.MethodOptions || r.Method == fiber.MethodConnect || r.Method == fiber.MethodTrace {
			continue
		}
		key := r.Method + " " + r.Path
		path := fill(r.Path, "d_bob")

		// 1. Signed out: refused (unless public).
		if !publicRoutes[key] {
			resp, err := app.Test(httptest.NewRequest(r.Method, path, nil))
			require.NoError(t, err, key)
			require.Contains(t, []int{http.StatusSeeOther, http.StatusUnauthorized}, resp.StatusCode, "anonymous %s", key)
		}

		// 2. Alice aiming at Bob's device: never reaches a handler.
		if !strings.Contains(r.Path, ":device_id") && !strings.Contains(r.Path, ":id") && !isDeviceScoped(r.Path) {
			continue
		}
		req := httptest.NewRequest(r.Method, path, nil)
		req.Header.Set(middleware.DeviceIDHeader, "d_bob")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(aliceCookie)
		resp, err := app.Test(req)
		require.NoError(t, err, key)
		require.Equal(t, http.StatusNotFound, resp.StatusCode, "alice -> bob's device via %s", key)
		checked++
	}
	require.Greater(t, checked, 60, "the route walk actually covered the upstream API (guard against an empty walk)")
}

// fill replaces every path parameter: device params get the target device,
// others a harmless placeholder.
func fill(path, device string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		switch {
		case p == ":device_id" || p == ":id":
			parts[i] = device
		case strings.HasPrefix(p, ":"):
			parts[i] = "x"
		case p == "*":
			parts[i] = "x"
		}
	}
	return strings.Join(parts, "/")
}

// isDeviceScoped: every route registered on the device group. They are all
// routes except our own pages, /devices management (covered by :device_id)
// and the public set.
func isDeviceScoped(path string) bool {
	for _, own := range []string{"/signin", "/signout", "/ui/static", "/auth/passkey", "/account", "/admin", "/health", "/devices"} {
		if strings.HasPrefix(path, own) {
			return false
		}
	}
	return path != "/" && path != "/app/info"
}

func csrfForRaw(rt *multiuser.Runtime, raw string) string {
	app := fiber.New()
	var tok string
	app.Get("/", func(c fiber.Ctx) error { c.Locals("wtf_session_raw", raw); tok = rt.Auth.CSRFToken(c); return nil })
	_, _ = app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	return tok
}
```

Two adjustments to make while running it:
- `GetRoutes(true)` filters out `Use` middleware. Confirm with `go doc github.com/gofiber/fiber/v3 App.GetRoutes`.
- Upstream routes like `/devices/:device_id/...` go through `ScopedDevices`, which returns `ErrDeviceNotFound` and becomes a 404 via `PanicIfNeeded` and Recovery. The device-group routes return 404 from `DeviceMiddleware`.
- If a route legitimately answers something else (for example `/app/info` has no device), add it to `publicRoutes` or to the `isDeviceScoped` exclusions **with a one-line reason comment**. Never widen the expected status set.

- [ ] **Step 2: Run it.** Run `cd src && go test ./cmd/ -run TestEveryRouteIsGatedAndOwnerScoped -v`. Expected: PASS, with `checked` well above 60 (upstream has about 90 device routes; set the floor to the real count minus 10 once you see it).

- [ ] **Step 3: Break-it checks (all three must go red).**
  1. Set `middleware.DeviceResolveFunc = nil` after `rt.Install(app)`. Upstream's resolver then hands Alice Bob's device and the request reaches a nil usecase.
  2. In `ScopedDevices.own`, return `nil`.
  3. Add a temporary route `app.Get("/leak", func(c fiber.Ctx) error { return c.SendString("x") })` *before* `rt.Install(app)`. The anonymous check must catch it.

  Revert all three.

- [ ] **Step 4: Commit.**

```bash
git add src/cmd/multiuser_isolation_test.go
git commit -m "test(wtf): every registered route is gated and owner-scoped for two users"
```

---

### Task 14: Render deployment (Blueprint) and the phase 1 go-live

**Files:**
- Create: `render.yaml`
- Modify: `readme.md` (a short "WTF fork" section at the top: what it is, `WTF_*` env vars, the admin-link bootstrap)

**Interfaces:** none.

- [ ] **Step 1: Write `render.yaml`.**

```yaml
services:
  - type: web
    name: wtf
    runtime: docker
    dockerfilePath: ./docker/golang.Dockerfile
    dockerContext: .
    plan: starter
    healthCheckPath: /health
    disk:
      name: data
      mountPath: /app/storages
      sizeGB: 1
    envVars:
      - key: APP_PORT
        value: "10000"
      - key: WTF_MULTIUSER
        value: "true"
      - key: WTF_SIGNUP_OPEN
        value: "false"
      - key: WTF_PRODUCT_NAME
        value: WTF
      - key: SESSION_SECRET
        generateValue: true
      - key: WTF_SUPERADMIN_PHONES
        sync: false
```

`WTF_PUBLIC_URL` is left unset: the app falls back to `RENDER_EXTERNAL_URL`. `/health` returns 503 until the device manager is healthy. Before trusting the health check, read `DeviceManager.IsHealthy` (device_manager.go:136). If it's false with zero devices, a fresh deploy would never pass. In that case add a `// WTF:` change so `/health` reports 200 when multi-user mode is on and the accounts store pings.

- [ ] **Step 2: Validate the Blueprint.** Load the `render-blueprints` skill and validate the file (`render blueprints validate` if the CLI is installed). Fix any finding.

- [ ] **Step 3: Full verification before shipping.** Load `superpowers:verification-before-completion`, then run `cd src && set -o pipefail && go vet ./... && go test ./... 2>&1 | tail -30`. Expected: all green except the known ffmpeg test. Then run `code-review-loop` on the branch and fix what it finds.

- [ ] **Step 4: Push and open the PR.** Run `git push -u origin wtf/phase-1` (branch only, no tags), then `gh pr create --repo arilewis93/wtf --base main` with a summary of phases 1a to 1d and the test evidence. CI (`wtf-ci`) must be green.

- [ ] **Step 5: Ask the user before creating anything on Render.** Creating the service costs about $7.25 a month and links a real phone, so get the user's go-ahead first. With it:
  1. Create the Blueprint from the repo's `main` after the PR merges, in the workspace the user names.
  2. The user types `WTF_SUPERADMIN_PHONES` into the dashboard. Never pass it through chat.
  3. Open the Render shell and run `./whatsapp admin-link <superadmin phone>`.
  4. The user opens the link, signs in, enrols a passkey, links the service phone on My WhatsApp, and picks it on `/admin/settings`.
  5. From another phone the user owns, send "Sign me in to WTF" to the service number and confirm the reply arrives.

- [ ] **Step 6: Measure memory per linked phone.** With the user, link phones one at a time (theirs first) and record `/admin/settings` memory after each one settles. Wait two minutes after the first sync. Put the table in the PR and set the phone cap for phase 4 from it (leave 30% headroom under 512 MB).

- [ ] **Step 7: Walk the real paths.** Load the `walk-user-paths` skill and walk two roles (superadmin, ordinary user created by `admin-link`) on the live site:
  - sign in by link, passkey enrol, passkey sign-in, link and unlink a phone
  - the admin disables a user and their open tab is bounced
  - the user tries another user's device id in `/devices/<id>` and gets a 404

- [ ] **Step 8: Commit and push the measurements.**

```bash
git add render.yaml readme.md
git commit -m "chore(wtf): Render Blueprint and fork readme"
git push
```

---

## Deviations from the design doc (tell the user)

1. **The superadmin is an env allowlist (`WTF_SUPERADMIN_PHONES`), not an `is_superadmin` column.** The user's standing rule (the `building-multitenant-saas-auth` skill) is that a database role is a privilege-escalation surface. `admin-link` mints the link, and the allowlist grants the powers. The doc's data-model row and render.yaml should be updated to match.
2. **The upstream `/ws` websocket is switched off instead of scoped per user.** Nothing in WTF's own UI needs it (htmx polls status every 3 seconds while linking). Its broadcasts carry every device's list, so filtering them safely means rewriting upstream's hub. If a live feed is wanted later, add a per-user one.
3. **MCP is off in phase 1.** Phase 2 turns it back on with per-user OAuth subjects, one-click connect and CIMD, together with personal API keys.
4. **Rename a device isn't in phase 1.** Upstream has no rename route. Cards show the phone number, which is unambiguous.
5. **Media downloads over REST return a server path that isn't served** in multi-user mode, because `/statics` is unmounted. Phase 2 adds owner-checked file fetching alongside API keys. That's harmless in phase 1, since only superadmins can sign in.
6. **Code-review finding fixed in passing:** `messages` lookups by id were unscoped (`GetMessageByID`). One account could download another account's media by message id, and `DeleteForMe` could delete across accounts. Task 5 scopes them.

## Self-review notes

- Spec coverage for phase 1: accounts.db (Tasks 1 and 2); WhatsApp sign-in through the service number (6); passkeys (8, 11); sessions (2, 3); `device_owners` (2, 4); the seven scoping changes (Basic Auth (12), device middleware (5), `/devices` (4), MCP (off, 1), websocket (off, 12), statics (off, 12), globals (1)); the cross-user test (13); deploy with only superadmins able to sign in (14, `WTF_SIGNUP_OPEN=false`); memory measurement (14). Phase 2 to 4 items (API keys, MCP per user, one-click connect, CIMD, 24-hour retention, waitlist, Delete my account) are deliberately out of scope.
- Type names checked across tasks: `store.User`, `auth.WithUser`/`UserFrom`, `owners.NewScopedDevices`, `owners.Resolver.Resolve`, `signin.Handler.ServiceDeviceID`, `web.Deps`, `multiuser.Setup`/`Install`/`Close`, `setupMultiUser(app, dm, s)`. `setupMultiUser` takes the settings loaded once at the top of `restServer`.
