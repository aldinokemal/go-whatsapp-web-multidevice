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
All controllers for the same phone must use the same lock-capable phone
`state_dir`: its `<serial>.lock` reserves the entire session. Different evidence
directories must never create independent reservations for the same phone.

```sh
mkdir -m 700 /private/recovery-state
/private/bin/gowa-companion --config /private/recovery.json --state-dir /private/recovery-state
/private/bin/gowa-companion --config /private/recovery.json --state-dir /private/recovery-state --account sales --apply
```

An example controller configuration using the built-in adapters follows. Replace
all paths and synthetic identity values. `not_before` is optional RFC3339 with a
timezone; an early apply inspects servers without phone input or pairing changes.
For adapters that can verify independent native companion IDs, list the primary
backend first, followed by standbys. Host locks are acquired in sorted backend-ID
order, while pairing follows configuration order. The built-in HTTP adapter
supports a single backend per account; see the identity limitation below.

```json
{
  "schema_version": 1,
  "accounts": [{
    "id": "sales",
    "phone": "15551234567",
    "display_name": "Example Sales",
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

The API's `/devices/{id}.jid` is the normalized **account JID**, without the
companion device component. It identifies the account but cannot prove that two
servers have independent companions. The HTTP adapter reports
`identity_scope: "account"`; multi-host workflows reject it with
`companion_identity_unavailable` before acquiring host locks or requesting codes.
For multiple backends, provide adapters that verify the sole matching full native
companion JID (`number:device@s.whatsapp.net`) against authenticated state, reject
ambiguous/mismatched stores, and return distinct IDs. Do not substitute host labels,
invent a device suffix, or treat normalized and full JIDs as independent identities.

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
Final multi-host acceptance requires every backend healthy with distinct full
companion JIDs.

Pairing cannot bypass WhatsApp restrictions, an account ban, or the provider's
account-review process. These conditions must not trigger repeated relinking.
The separate opt-in provider-review mode below can request an official review;
verification still needs owner participation. A successful recovery is a point-in-time result:
the provider can revoke those companions again afterward.

### Immediate official account reviews

`--mode provider-review` is separate from pairing. Enable `auto_appeal: true` on
the intended account and use an adapter implementing `review_begin`,
`review_request`, and `finish`. The bundled `adb-phone` adapter implements them.

`review_begin` binds the enrolled primary phone, Android user and official app to
the configured account, returning `identity_verified: true`, `identity_source`
and private `xml` over the existing pipes. The adapter has already checked the
serial, Android user and foreground package. A ban screen often hides the
profile, so it does not navigate to Settings: `identity_source` is
`screen_number` when the account's number is visible, otherwise
`device_binding`. Any other phone number on screen fails with `identity_mismatch`.

`review_request` receives the SHA-256 of the screen returned by `review_begin`
and refuses any other hash. It reads the current screen again; only when that
screen still has the unique, enabled **REQUEST REVIEW** button on a recognized
ban or restriction screen does it tap once, wait, and return the new XML with
`tapped: true`. Otherwise it returns the current XML with `tapped: false` and
sends no input. A session taps at most once and never types, navigates or
pairs. `finish` only turns the display back off if it was off.
Raw XML, codes and message contents must not enter controller logs.

With `--apply`, an available review is requested immediately, even if pairing's
`not_before` or six-hour restriction cooldown has not elapsed. Without `--apply`,
this mode examines the phone but never presses the review button. Ordinary
recovery inspection continues to avoid phone access.

The controller records an attempt durably **before** the click. Pending manual
appeals are adopted without another click; crashes, unfamiliar outcomes and
authentication interruptions cannot cause duplicate submissions on restart.
`verification_required` and `needs_owner` identify a prompt needing immediate
owner attention, including SMS verification required **before** an appeal.
An unconfirmed click is never reported as a submitted appeal, and
`submitted_now` is reported only when this run actually tapped. Unknown forms and
custom appeal text are not filled automatically. Resolve an interrupted attempt
by inspecting the official app, rather than deleting its durable intent.

Review state uses `provider-review` and `provider-review-attempt` keys in the
existing account-locked state store. A healthy primary/session resolves that ban
episode. Already-pending reviews are checked at five-minute intervals; this wait
does not apply before the first appeal. Six hours is not a guaranteed provider
review duration. Polling/owner notifications are installation-specific.

Codes pass through process pipes and one-character phone input with pauses. The
controller suppresses adapter stderr and reports only bounded symbolic errors.
GoWA itself may log pairing codes; protect its logs separately. PNG/XML evidence
can contain private conversations. The local ADB adapter writes it under the
configured private directory, restores the starting app screen and sleep state,
and releases its reservation. It refuses another foreground app/profile or a
protected lock screen; it does not bypass owner authentication.
Before capture, it wakes the display and rechecks the foreground app/profile and
keyguard so a display timeout cannot silently interrupt confirmation or cleanup.

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
Adapters run outside the controller's terminal process group. Cancellation leaves
server adapters alive until phone cleanup finishes. An interrupted or malformed
phone stream is closed with EOF rather than reused: cooperative adapters cancel
the active operation and restore/release before exiting. Cleanup has a bounded
grace period; an unresponsive adapter is then terminated and recovery stays
blocked. External adapters must implement equivalent EOF cleanup behavior.

Server operations (`device`, `phone` on every request):

- `inspect`: return boolean `connected`, `logged_in`, `policy_ok`, nonnegative
  integer `saved_sessions`, and string `companion_jid` (empty when logged out).
  Multi-host adapters must provide full native companion IDs after authentication;
  account-only adapters must advertise `identity_scope: "account"` even while
  logged out, so unsupported recovery stops before pairing.
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
removing another linked application. After code submission, the recognized Device
name form (known explanatory text, one blank enabled field and a single SAVE
button) receives the generic label `GOWA` through character input. The driver
verifies the entered label before saving once and still requires Linked devices
confirmation. Populated, ambiguous or unfamiliar naming forms stop for review.
Other versions, languages, accessibility trees and OEM dialogs require separate
validation; unknown UI fails closed. Full slots require separate operator review;
the controller never deletes an unrelated or retired slot automatically.

Synthetic tests cover workflow ordering, durable interruption and partial recovery,
reconnect, shared locks, cooldowns, identity and policy checks, UI regressions,
protocol bounds, HTTP security, and cleanup. Run from `src/`:

```sh
go test -race ./pkg/companion ./tools/companion
go test -tags purego ./pkg/companion ./tools/companion
go vet ./pkg/companion ./tools/companion
```

Operator-assisted live acceptance drove two independent code-based pairings with
the Go controller through pre-existing external deployment/phone adapters. After
authentication, independent native IDs, all per-host line policies, unchanged
active/standby ownership, integration health and phone cleanup were verified; a
final Go apply returned healthy and resolved the authenticated pairing intents.
No customer messages, calls or failed-message replays were used as canaries.
That account was subsequently banned by the provider and its companions remotely
logged out again. The earlier acceptance remains historical evidence, not a claim
that the account is currently healthy or that this controller fixes provider bans.

This was not fully unattended UI acceptance: an operator prepared a full-slot
phone by removing a separately verified obsolete logged-out entry. The phone
adapter initially timed out on the post-link naming form, and native authenticated
state was independently checked afterward without requesting another code. The
generic Go ADB/UI implementation, including wake and naming regressions, has
**synthetic validation only**; the live run used external adapters. An earlier
blocked attempt does not count as successful acceptance. Preserve healthy sessions
when validating other Android versions; never revoke a companion to manufacture
a test.
