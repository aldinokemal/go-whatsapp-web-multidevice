# Optional Android companion recovery

`tools/companion` is a standalone, bounded Go controller for recovering an existing
GoWA device after confirmed remote logout. It reuses the device status, reconnect,
and phone-number pairing APIs. It does not start the REST service, read its saved
sessions directly, or grant that service ADB permissions. It never sends messages
or calls, removes linked devices, restarts services, or changes host ownership.

Build from `src/` (Unix host required for process locks):

```sh
go build -o /private/bin/gowa-companion ./tools/companion
```

Run without `--apply` to inspect servers only. This starts no phone process and
requests no code. `--apply` enables one bounded recovery workflow; it is not a
background retry loop. Each account needs an explicit device ID, expected display
name **and international phone number**, and an explicit phone/profile adapter.
The generic ADB adapter requires an explicit serial and Android user, including
user `0` for the main profile. Configuration, state and evidence are private.

```sh
mkdir -m 700 /private/recovery-state
/private/bin/gowa-companion --config /private/recovery.json --state-dir /private/recovery-state
/private/bin/gowa-companion --config /private/recovery.json --state-dir /private/recovery-state --account sales --apply
```

An example controller configuration using the built-in adapters follows. Replace
all paths and synthetic identity values. `not_before` is optional RFC3339 with a
timezone; an early apply inspects servers without phone input or pairing changes.
List the primary backend first, followed by independent standbys. Host locks are
acquired in sorted backend-ID order, while pairing follows configuration order.

```json
{
  "schema_version": 1,
  "accounts": [{
    "id": "sales",
    "phone": "15551234567",
    "display_name": "Example Sales",
    "not_before": "2099-01-01T12:00:00Z",
    "verify_seconds": 60,
    "backends": [{
      "id": "primary",
      "command": ["/private/bin/gowa-companion", "--mode", "http-backend", "--config", "/private/backend.json"]
    }],
    "phone_adapter": {
      "command": ["/private/bin/gowa-companion", "--mode", "adb-phone"],
      "config": {
        "serial": "EXAMPLE_SERIAL",
        "user": 0,
        "package": "com.whatsapp.w4b",
        "state_dir": "/private/phone-state"
      }
    }
  }]
}
```

Example HTTP backend configuration:

```json
{
  "url": "http://127.0.0.1:3000",
  "device": "sales",
  "phone": "15551234567",
  "basic_auth_env": "GOWA_BASIC_AUTH",
  "lock_file": "/private/recovery-state/primary.lock",
  "confirmed_remote_logout": false
}
```

Set `confirmed_remote_logout` to true only after independently establishing that
native credentials were revoked. HTTP cannot prove credential absence. Its safe
default first attempts ordinary reconnect and then stops for operator review.
Configure separate lock files for separate servers, shared by all controllers
that can act on that server. The HTTP adapter requires HTTPS for remote hosts,
disables proxies and redirects, validates the account JID, and scopes every
request with `X-Device-Id`. Basic credentials come from the named environment
variable as `username:password`; they do not belong in the URL or argv.

The generic HTTP adapter does not implement deployment-specific call admission,
HA claims, or native database checks. Deployments with those requirements should
supply their own adapter that retains the existing deployment lock and verifies
policy throughout the workflow. The JSON-lines protocol lets existing adapters
remain separate; no SSH, fleet inventory, or deployment adapter is bundled here.

## Safety and durable state

The controller uses a per-account `flock` for the whole workflow and keeps host
adapters alive until phone cleanup finishes. The phone adapter verifies both the
real profile display name and actual phone number before preparing any pairing.
The phone must reach a recognized code-entry screen before the controller commits
a durable SQLite pairing intent, **before requesting a code**. Each backend gets
at most one request in that attempt. A crash, timeout or uncertain confirmation
leaves that intent in place and blocks automatic identity creation on the next
run. There is deliberately no reset/retry shortcut. A healthy, verified session
can resolve an old intent through normal inspection. Never delete state to retry.
The SQLite `recovery` schema and `<account>.lock` convention are compatible with
the original JSON-lines controller, so the same private state directory can be
used during migration; do not run controllers with independent state copies.

Existing credentials get a reconnect attempt before pairing is considered. Unknown
screens, ambiguous identity or device rows, full slots, owner PIN/OTP/verification,
restrictions, policy changes, and busy reservations stop the workflow. A restriction
countdown persists with a five-minute margin, or a six-hour cooldown when no
countdown is accessible. A failed cleanup also blocks success and retains any
cooldown. A prior cleanup failure requires operator review even when server sessions
subsequently become healthy. A partially recovered multi-host account is not a successful recovery.
Final acceptance requires every backend healthy with distinct companion JIDs.

Codes pass through process pipes and one-character phone input with pauses. The
controller suppresses adapter stderr and reports only bounded symbolic errors.
GoWA itself may log pairing codes; protect its logs separately. PNG/XML evidence
can contain private conversations. The local ADB adapter writes it under the
configured private directory, restores the starting app screen and sleep state,
and releases its reservation. It refuses another foreground app/profile or a
protected lock screen; it does not bypass owner authentication.

## Adapter protocol

Each adapter is one long-lived process with one JSON request/reply per line.
Commands are argv arrays, never shell fragments evaluated by the controller.
Stdout is protocol only. Requests contain `op` plus operation values. Replies are
`{"ok":true,"result":{...}}` or
`{"ok":false,"reason":"restriction","retry_after":21600}`. Error prose and
unknown reasons are discarded. Replies are bounded to 64 KiB and calls have a
180-second deadline; finish has a separate 60-second cleanup deadline. EOF must
release locks/reservations and restore the phone. Adapters must bound abandoned
remote lock leases independently.

Server operations (`device`, `phone` on every request):

- `inspect`: return boolean `connected`, `logged_in`, `policy_ok`, nonnegative
  integer `saved_sessions`, and string `companion_jid` (empty when logged out).
- `acquire`: obtain the deployment/host lock and retain it until process exit.
- `reconnect`: reconnect existing credentials under the lock.
- `pair_code`: request one code, returning `pair_code`; refuse existing credentials
  and repeat requests within the process.

Phone operations:

- `begin`: receive `account` (`id`, `phone`, `display_name`) and adapter `config`;
  reserve the whole UI session, verify real identity, return `identity_verified:true`.
- `prepare_pair`: receive `backend`; reach code-entry UI, return `ready:true`.
- `enter_code`: receive `code` via stdin, submit once and wait for acceptance.
- `finish`: restore the prior app/display and release the reservation, including
  when preparation or input failed.

Read the JSON `state`, not just the exit code: `inspection`, `waiting`, and
`healthy` exit 0; `blocked` exits 2. Per-account records identify the blocker.
No scheduler is installed by this tool. Schedule only explicitly authorized,
bounded attempts, using the same durable state and reservations.

## Android compatibility and validation

The UI driver currently recognizes the English WhatsApp/Business Android UI. It
uses accessibility XML labels and observed bounds, not fixed screen coordinates.
It recognizes the selected Chats tab when there is no Chats title, and enabled
popup ListView rows even when Android reports `clickable=false`. It restores an
unambiguously logged-out GoWA row through LOG IN, or uses LINK A DEVICE without
removing another linked application. Other versions, languages, accessibility
trees and OEM dialogs require separate validation; unknown UI fails closed.

Synthetic tests cover workflow ordering, durable interruption and partial recovery,
reconnect, shared locks, cooldowns, identity and policy checks, UI regressions,
protocol bounds, HTTP security, and cleanup. Run from `src/`:

```sh
go test -race ./pkg/companion ./tools/companion
go test -tags purego ./pkg/companion ./tools/companion
go vet ./pkg/companion ./tools/companion
```

Synthetic tests and inspection do not establish real Android pairing compatibility.
Validate live pairing only in an authorized recovery, preserving healthy sessions
and unrelated linked devices; never revoke a healthy companion to manufacture a test.
