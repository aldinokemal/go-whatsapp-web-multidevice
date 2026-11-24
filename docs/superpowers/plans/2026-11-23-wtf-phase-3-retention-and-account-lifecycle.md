# WTF Phase 3: 24-Hour Retention, Delete My Account and Device Names Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each task names its kind, model, files, interfaces, layered tests and dependencies, so it can be cut straight into a builder brief (`captain-and-crew/brief-template.md`).

**Goal:** Before strangers can sign up (phase 4), make WTF keep as little as possible and let people leave: message content and media are kept for 24 hours and then deleted, a person can delete their account and everything tied to it in one step, and a person can name each linked number.

**Architecture:** A retention worker in `src/multiuser/retention/` runs at start-up and hourly. It calls one new device-agnostic purge method on the chat storage repository (added through all three layers, as `AGENTS.md` requires), sweeps the media and send-item folders by age, and prunes expired rows in `accounts.db`. A small hook stops history sync from storing messages already past the window. Account deletion is a service in `src/multiuser/account/` that marks the user `deleting` first (so every session, key and token dies on the next request), then purges devices through the existing `PurgeDevice`, revokes OAuth grants and keys, deletes the user row (cascades do the rest) and resumes itself at start-up if interrupted. Device names are a `label` column on `device_owners`, shown wherever the number is shown.

**Tech Stack:** as phases 1 and 2.

**Spec:** "GOWA multi-user fork: design", https://claude.ai/code/artifact/24e3a973-7165-4898-a922-a8edb24b9824. **Not readable when this plan was written (2026-11-23).** The phase split below is inferred: the phase 1 plan lists "24-hour retention", "Delete my account" and "rename a device" among the phase 2 to 4 items without assigning them; this plan groups the privacy and lifecycle items ahead of opening sign-up. Builders who can open the spec read its phase 3 section first and tell the captain about any difference before building.

**Skills every builder loads first:** superpowers:test-driven-development with `layering-tests-by-what-they-check`; `building-multitenant-saas-auth`; `securing-multitenant-saas` (destructive operations, audit); for UI tasks `frontend-house-style`, `ui-ux-checklist` and `docs/ui.md`. Read `src/infrastructure/chatstorage/AGENTS.md` before Task 1.

**Depends on phase 2 being merged:** account deletion revokes API keys (`RevokeAllAPIKeys`) and OAuth grants (`oauth.Store.RevokeSubject`), and the label appears in the `wtf_my_devices` MCP tool.

## Global Constraints

- Everything in the phase 1 and phase 2 Global Constraints still holds.
- **Retention applies only in multi-user mode.** Upstream behaviour is unchanged with `WTF_MULTIUSER` off: the hook vars stay nil and no worker starts.
- **Retention window:** `WTF_RETENTION_HOURS`, default `24`, minimum `1`, `0` means off (refused when `WTF_SIGNUP_OPEN=true`, so an open server always has a window). Ages are compared in UTC against the message's own timestamp, never the server's wall clock in another zone.
- **What is deleted after the window:** messages and their dependent rows (`message_edits`, `message_reactions`, `poll_definitions`, `chatwoot_message_links`), chats whose newest message is past the window, downloaded media files and send-item files whose modification time is past the window. **What is kept:** device sessions and keys (whatsmeow store), contacts and push names, `scheduled_sends` that have not run yet, the accounts database (users, passkeys, sessions, keys, owners, settings) and the audit log for 90 days.
- **Deletion is a positive act.** A purge runs only on an explicit, parsed, positive window; a zero, negative or unparsable value never means "delete everything" (`securing-multitenant-saas` gap #20).
- **Audit rows never carry a phone number or message text.** `account_deleted` records the user id only.
- **Migrations:** accounts-store migrations are appended in Task 0 only; the chat storage migration (if an index is needed) is appended in Task 1 only, to `getMigrations()` in `sqlite_repository.go`.
- **UI** uses the redesigned design system (`docs/ui.md`), as in phase 2.

## Review Focus

1. **A purge that deletes too much.** A message one minute inside the window survives; a chat with one fresh message and many old ones keeps the chat and the fresh message; a pending scheduled send survives; another table that references messages is not left dangling. Pinned in Task 1.
2. **History sync re-importing what retention just deleted.** Linking a phone pulls months of history; none of it older than the window is stored, and a later purge does not race an in-flight batch into a half-state. Pinned in Task 3.
3. **Account deletion interrupted halfway.** A crash after the devices are purged but before the user row is deleted must not leave a signed-in, half-deleted account: the user is `deleting` (refused everywhere) from the first step, and start-up finishes the job. Pinned in Task 4.
4. **Deleting the account that owns the service number.** That would stop sign-in for everyone. Refused with a clear message until another number is picked. Pinned in Task 4.
5. **A label as an injection point.** Labels are user text rendered in HTML, JSON and MCP output; `html/template` escapes them, length and characters are bounded, and another user's device cannot be labelled. Pinned in Task 6.

---

## File Structure

```
src/multiuser/
  config.go                    + RetentionHours (Task 0)
  store/
    store.go                   + migrations: device_owners.label (Task 0)
    users.go                   + StatusDeleting, DeleteUser, UsersWithStatus (Task 0)
    owners.go                  + SetLabel, Label(s) (Task 0)
    prune.go                   PruneExpired (login tokens, sessions, audit > 90 days) (Task 0)
  retention/
    retention.go               Worker: Run(ctx), RunOnce(ctx, now) (Task 2)
    files.go                   SweepDir(root, cutoff) (Task 2)
    retention_test.go, files_test.go
  account/
    delete.go                  Deleter: Delete(ctx, userID), ResumePending(ctx) (Task 4)
    delete_test.go
  owners/devices.go            + Label filled into domain Device (Task 6)
  web/
    account.go                 + delete account route (Task 5)
    devices.go                 + rename routes (Task 7)
    templates/account.html     + "Delete account" card (Task 5)
    templates/home.html        + device name on each row, Rename in the row (Task 7)
    templates/rename_device.html  dialog body (Task 7)
    preview_test.go            + fixtures (Tasks 5, 7)
  runtime.go                   wiring (Task 8 only)
src/domains/chatstorage/interfaces.go          + PurgeMessagesBefore (Task 1)
src/infrastructure/chatstorage/sqlite_repository.go  + PurgeMessagesBefore (Task 1)
src/infrastructure/whatsapp/chatstorage_wrapper.go   + PurgeMessagesBefore passthrough (Task 1)
src/infrastructure/whatsapp/history_sync.go    // WTF: HistoryCutoffHook (Task 3)
src/domains/device/device.go                    + Label `json:"label,omitempty"` (Task 6)
src/cmd/multiuser_isolation_test.go             + label and delete-account cases (Task 8)
readme.md, render.yaml                          + WTF_RETENTION_HOURS (Task 8)
```

---

## Task order and parallelism

```
T0 config + store ──┬─► T1 chatstorage purge ──┬─► T2 retention worker ──┐
                    │                          └─► T3 history cutoff ────┤
                    ├─► T4 account deletion ──► T5 UI: delete account ───┼─► T8 wiring, isolation, docs ─► (T9 optional live feed)
                    └─► T6 device labels ─────► T7 UI: rename ───────────┘
```

| Group | Tasks | Runs in parallel with | Shared-file reason for any ordering |
|---|---|---|---|
| A | T0 | nothing | owns `config.go` and the accounts-store migrations |
| B | T1, T4, T6 | each other | T1 owns the three chatstorage layers; T4 owns `multiuser/account/`; T6 owns `owners/devices.go`, `domains/device/device.go`, `store/owners.go` was done in T0 |
| C | T2, T3 | each other, T4, T6 | both after T1; T2 owns `retention/`; T3 owns the `history_sync.go` hook |
| D | T5, T7 | backend tasks | both edit `preview_test.go` and the shared layout; run T5 then T7 |
| E | T8 | nothing | `runtime.go`, `cmd/*`, isolation test, docs |
| F | T9 (optional) | nothing | only if Aryeh says yes |

**Sizing:** 9 tasks (T0 to T8) plus 1 optional (T9). Complexity: T0 S, T1 M, T2 M, T3 S, T4 L, T5 M, T6 S, T7 M, T8 M, T9 L. Highest risk: T4 (irreversible, multi-store, must resume), T1 (a delete query that must not over-reach), T2 (a worker deleting files).

**Models:** opus for T1, T2, T3, T4, T8 (data deletion, authorisation). sonnet for T0, T5, T6, T7. T9 opus (per-user event scoping).

---

### Task 0: Settings and accounts-store additions

**Kind:** backend. **Model:** sonnet. **Depends on:** phase 2 merged. **Parallel:** none.

**Files:**
- Modify: `src/multiuser/config.go`, `src/multiuser/config_test.go`, `src/multiuser/store/store.go`, `src/multiuser/store/users.go`, `src/multiuser/store/owners.go`
- Create: `src/multiuser/store/prune.go`, `src/multiuser/store/prune_test.go`, `src/multiuser/store/lifecycle_test.go`

**Interfaces:**
- `Settings.RetentionHours int`: `WTF_RETENTION_HOURS`, default 24; invalid or negative is a `Load` error; `0` allowed only when sign-up is closed (`Load` error otherwise); `(Settings) RetentionWindow() (time.Duration, bool)`.
- Migration appended: `ALTER TABLE device_owners ADD COLUMN label TEXT NOT NULL DEFAULT ''`.
- `store.StatusDeleting = "deleting"`. `SessionUser`, `UserByAPIKey` and the OAuth `Active` check already refuse anything but `active`; add a test that pins this for `deleting`.
- `(*Store).DeleteUser(ctx, id) error` (the row; `ON DELETE CASCADE` removes sessions, passkeys, owners, keys). Turn on `PRAGMA foreign_keys=ON` if `Open` does not already (check `FormatChatStorageURI(uri, true, true)`'s second argument; add a test either way).
- `(*Store).UsersWithStatus(ctx, status) ([]User, error)`.
- `(*Store).SetLabel(ctx, userID, deviceID, label string) error` (`ErrNotFound` unless the user owns the device), `(*Store).LabelsOf(ctx, userID) (map[string]string, error)`.
- `(*Store).PruneExpired(ctx, auditKeep time.Duration) (PruneCounts, error)`: deletes used or expired `login_tokens`, expired `sessions`, `audit_log` rows older than `auditKeep`.

- [ ] **Step 1: Failing tests.** Behaviour: config parsing table (24 default; `"0"` with sign-up closed ok; `"0"` with sign-up open error; `"-1"`, `"abc"` error); `TestDeletingUserCannotUseASession`; `TestDeleteUserCascades` (rows in sessions, passkeys, device_owners, api_keys for that user all gone; another user's rows untouched); `TestSetLabelOnAnotherUsersDeviceIsNotFound`; `TestPruneKeepsLiveRows` (a live session and an unused unexpired token survive; frozen clock).
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Break-check** `TestDeleteUserCascades` by turning foreign keys off in the test DSN: red. Restore.
- [ ] **Step 4: Commit.** `git commit -m "feat(store): retention setting, deleting status, device labels, pruning"`

---

### Task 1: Chat storage: one device-agnostic purge

**Kind:** backend (data deletion). **Model:** opus. **Depends on:** T0. **Parallel:** with T4, T6.

**Files:**
- Modify: `src/domains/chatstorage/interfaces.go`, `src/infrastructure/chatstorage/sqlite_repository.go`, `src/infrastructure/whatsapp/chatstorage_wrapper.go` (all three, per `AGENTS.md`); any test doubles that implement the interface (find with `grep -rn "IChatStorageRepository" src --include=*_test.go`)
- Create: `src/infrastructure/chatstorage/purge_test.go`

**Interfaces:**
- `PurgeMessagesBefore(cutoff time.Time) (PurgeResult, error)` on `IChatStorageRepository`, with `type PurgeResult struct{ Messages, Chats int64 }` in `domains/chatstorage`.
- In one transaction, in batches of at most 5,000 message rows per statement (so a first run after deploy on a large database does not hold the write lock for long; loop until a batch deletes fewer than the batch size, committing per batch):
  1. Select the keys `(id, chat_jid, device_id)` of messages with `timestamp < cutoff` (check the column name and type in `getMigrations()`; compare in UTC).
  2. Delete their `message_reactions`, `message_edits`, `chatwoot_message_links` (by `wa_message_id`, `device_id`) and `poll_definitions` (by `poll_message_id`, `chat_jid`, `device_id`).
  3. Delete the messages.
  4. Delete `chats` rows that have no remaining messages **and** whose `last_message_time` (check the name) is before the cutoff.
- Never touch `scheduled_sends`, `devices`, `chatwoot_device_configs`, `chatwoot_forward_queue` rows for messages still present.
- If the timestamp column has no index, append a migration adding `CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp)`.
- The wrapper passes through unchanged.

- [ ] **Step 1: Failing tests (real SQLite in a temp dir).** Behaviour: `TestPurgeDeletesOnlyOldMessages` (two devices, messages at cutoff-1s, cutoff, cutoff+1s; only the first goes; the boundary is "strictly before"); `TestPurgeRemovesDependentRows` (an old message with an edit, a reaction, a poll definition and a chatwoot link: all four gone; the same rows for a fresh message stay); `TestPurgeKeepsAChatWithAFreshMessage`; `TestPurgeRemovesAnEmptyOldChat`; `TestPurgeNeverTouchesScheduledSends`. Mechanism: `TestPurgeBatchesLargeBacklogs` (12,001 old rows: all gone; and with a test-only batch size of 100 the function commits more than once, asserted through a counter seam, not timing). Shape: `TestEveryRepositoryImplementsPurge` is the compiler; also run `go vet ./...` on the three packages.
- [ ] **Step 2: Implement** with parameterised SQL; fixed fragments only.
- [ ] **Step 3: Break-check.** Change `<` to `<=`: the boundary test goes red. Drop the reactions delete: dependent-rows test red. Restore.
- [ ] **Step 4: Run** `cd src && go test ./infrastructure/chatstorage/... ./infrastructure/whatsapp/... ./domains/... -count=1`, plus `-tags purego` for the chatstorage package.
- [ ] **Step 5: Commit.** Stage the three layers and the test by name. `git commit -m "feat(chatstorage): purge messages older than a cutoff"`

---

### Task 2: The retention worker

**Kind:** backend (data deletion). **Model:** opus. **Depends on:** T1. **Parallel:** with T3, T4, T6.

**Files:**
- Create: `src/multiuser/retention/retention.go`, `src/multiuser/retention/files.go`, `src/multiuser/retention/retention_test.go`, `src/multiuser/retention/files_test.go`

**Interfaces:**
- `type Worker struct { Window time.Duration; Chats interface{ PurgeMessagesBefore(time.Time) (chatstorage.PurgeResult, error) }; Accounts interface{ PruneExpired(ctx, time.Duration) (store.PruneCounts, error) }; FileRoots []string /* "statics/media", "statics/senditems" */; Now func() time.Time; Every time.Duration /* 1h */ }`
- `(*Worker) RunOnce(ctx, now time.Time) (Report, error)`: cutoff = `now.UTC().Add(-Window)`; refuses to run if `Window <= 0` (returns an error, deletes nothing); purges chats, sweeps each root, prunes accounts with `auditKeep = 90 * 24h`. Logs counts only (no paths with phone numbers, no message ids).
- `(*Worker) Run(ctx)`: `RunOnce` at start, then every `Every`, until `ctx` is cancelled; a failing run logs and waits for the next tick; a run never overlaps the previous one.
- `SweepDir(root string, cutoff time.Time) (removed int, err error)`: walks with `filepath.WalkDir`, never follows symlinks (`d.Type()&fs.ModeSymlink` is skipped, not followed, not deleted), deletes regular files with `ModTime < cutoff`, then removes now-empty directories below `root` (never `root` itself). A missing root is not an error.

- [ ] **Step 1: Failing tests.** Behaviour: `TestRunOnceUsesTheWindow` (fake chats store records the cutoff: exactly `now - 24h` in UTC even when `now` is in another zone); `TestZeroWindowDeletesNothing`; `TestSweepRemovesOnlyOldFiles` (set mtimes with `os.Chtimes`); `TestSweepNeverFollowsSymlinks` (a symlink in the media root pointing at a directory of old files outside it: the outside files survive); `TestSweepRemovesEmptyDirsButNotTheRoot`. Mechanism: `TestRunDoesNotOverlap` (a fake purge that blocks; two ticks; one call in flight at a time) and `TestRunStopsOnCancel`.
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Break-check** the symlink test by following links: red. Restore.
- [ ] **Step 4: Commit.** `git commit -m "feat(multiuser): 24-hour retention worker"`

---

### Task 3: History sync does not store what is already past the window

**Kind:** backend. **Model:** opus. **Depends on:** T1 (shares nothing, but its tests use the purge as the reference). **Parallel:** with T2, T4, T6.

**Files:**
- Modify (hook, `// WTF:`): `src/infrastructure/whatsapp/history_sync.go` (around `processConversationMessages`, before `StoreMessagesBatch` at line ~334)
- Create: `src/infrastructure/whatsapp/history_sync_cutoff_test.go`

**Interfaces:**
- `var HistoryCutoffHook func() (cutoff time.Time, ok bool) // WTF: multi-user retention`. When set and ok, messages with a timestamp before `cutoff` are dropped from the batch before storing; chats whose every message was dropped are not created. Push names and contacts are still processed.
- Live messages are untouched (a live message is never older than the window).

- [ ] **Step 1: Failing tests.** Behaviour: `TestHistorySyncSkipsOldMessagesWhenHookSet` (a synthetic `HistorySync` with old and fresh messages; only fresh ones stored; use the existing history-sync test helpers); `TestHistorySyncUnchangedWithoutHook` (all stored); `TestHistorySyncStillStoresPushNames`.
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Commit.** `git commit -m "feat(whatsapp): history sync honours the retention window"`

---

### Task 4: Account deletion service

**Kind:** backend (irreversible, multi-store). **Model:** opus. **Depends on:** T0; phase 2 (`RevokeAllAPIKeys`, `RevokeSubject`). **Parallel:** with T1, T2, T3, T6.

**Files:**
- Create: `src/multiuser/account/delete.go`, `src/multiuser/account/delete_test.go`

**Interfaces:**
- ```go
  type Deleter struct {
      Store        *store.Store
      Devices      interface{ RemoveDevice(ctx context.Context, id string) error } // the inner (unscoped) usecase; PurgeDevice under it
      Grants       interface{ RevokeSubject(ctx context.Context, subject string) error }
      MediaRoot    string // statics/media; per-device dirs from phase 2
      ServiceDevice func(ctx context.Context) string
      Now          func() time.Time
  }
  var ErrOwnsServiceNumber = errors.New("account owns the service number")
  func (d *Deleter) Delete(ctx context.Context, userID string) error
  func (d *Deleter) ResumePending(ctx context.Context) error // start-up: finish every user left in "deleting"
  ```
- `Delete`, in this order, each step idempotent:
  1. If the user owns the current service device: `ErrOwnsServiceNumber`, nothing changed.
  2. `SetUserStatus(deleting)`. From here every session, key and token is refused (phase 1 and 2 checks).
  3. `DeleteUserSessions`, `RevokeAllAPIKeys`, `Grants.RevokeSubject(userID)`.
  4. For each owned device: `Devices.RemoveDevice` (logs out of WhatsApp best-effort, purges chat data and the slot); a "not found" counts as done; then `os.RemoveAll(<MediaRoot>/<deviceID>)` after checking the joined path is directly under `MediaRoot`; then `RemoveOwner`.
  5. Phase 4 adds: delete the person's waitlist row (leave a `// phase 4:` note; do not reference the table yet).
  6. `Audit{UserID: id, Action: "account_deleted"}` then `DeleteUser`.
  - Any error after step 2 leaves the user in `deleting` and returns the error; `ResumePending` (and the next `Delete` call) carries on from where it stopped.
- The scoped `owners.ScopedDevices` is not used here (the user may already be refused by it); ownership comes from `store.DevicesOf(userID)`.

- [ ] **Step 1: Failing tests (real accounts store; fakes for devices and grants that record calls and can be told to fail once).**
  - Behaviour: `TestDeleteRemovesEverything` (sessions, passkeys, keys, owners and the user row gone; RemoveDevice called for each owned device and none other; grants revoked for this subject only; the media dir for each device removed and another user's untouched; one `account_deleted` audit row with no phone); `TestOwnsServiceNumberIsRefusedAndChangesNothing`.
  - Mechanism: `TestInterruptedDeleteIsRefusedThenResumed` (devices fake fails on the second device: `Delete` returns an error, the user is `deleting`, `SessionUser` refuses their old session, `UserByAPIKey` refuses their key; then `ResumePending` finishes: user row gone, RemoveDevice called for the remaining device, the already-removed one tolerated); `TestDeleteIsIdempotent` (calling twice is fine).
  - Security: `TestMediaPathMustStayUnderRoot` (an owner row with a device id `../x` is refused for the file step, logged, and does not delete outside the root).
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Break-check.** Move `SetUserStatus(deleting)` after the device loop: the interrupted test goes red (the old session still works). Restore.
- [ ] **Step 4: Commit.** `git commit -m "feat(multiuser): delete an account and everything tied to it"`

---

### Task 5: UI: Delete my account

**Kind:** UI. **Model:** sonnet. **Depends on:** T4. **Parallel:** with backend tasks. **Sequential with:** T7 (shared `preview_test.go`).

**Files:**
- Modify: `src/multiuser/web/account.go`, `src/multiuser/web/templates/account.html`, `src/multiuser/web/account_test.go`, `src/multiuser/web/preview_test.go`, `src/multiuser/web/web.go` (`Deps.DeleteAccount func(ctx, userID string) error` only)

**Design:**
- Last card on `/account`: "Delete account", `.card` with `.card-sub` "Removes your account, unlinks every WhatsApp number and deletes their messages. This can't be undone." Button `btn-danger` "Delete account" (`{{icon "trash-2"}}`) top-right of the card head, opening a `confirm`-style dialog.
- The dialog cannot use the generic `confirm` template as-is because it needs a typed confirmation. Extend `confirm` in `layout.html` with an optional `"Typed"` field (label and expected value) rather than hand-drawing a second dialog (S10): when set, it renders one `.field` "Type the last 4 digits of your number" (`inputmode="numeric"`, `autocomplete="off"`, 16px on touch) and the destructive button stays enabled; the server checks the value. Cancel is focused first (existing behaviour).
- `POST /account/delete` (session, CSRF): wrong digits re-open the dialog with `.form-error` "Those digits don't match your number."; `ErrOwnsServiceNumber` shows "This number is the service number. Pick another one in Settings first." (superadmins only can hit this; the message names where to go); success ends the session (`EndSession`) and 303s to `/signin?deleted=1`, where the sign-in page shows a `.notice` "Your account was deleted."
- Hidden (not greyed) while the user is in `deleting` (they cannot reach the page anyway).

- [ ] **Step 1: Failing tests.** Behaviour: `TestDeleteNeedsTheRightDigits`; `TestDeleteSignsOutAndRedirects`; `TestServiceNumberOwnerSeesWhereToGo`. Security: `TestDeleteNeedsCSRF`; `TestDeleteRefusesAPIKeyAuth` (bearer, no cookie: 303 to sign-in, nothing deleted). Shape: `TestEveryDestructiveFormUsesTheConfirmTemplate` (if a shape test for this exists, extend it; if not, add one that scans templates for `btn-danger` buttons outside a `<dialog>`).
- [ ] **Step 2: Implement, preview fixtures** (card, dialog, dialog with error, service-number error, the signed-out notice) and screenshots at phone and desktop, light and dark.
- [ ] **Step 3: Commit.** `git commit -m "feat(web): delete my account"`

---

### Task 6: Device names (backend)

**Kind:** backend. **Model:** sonnet. **Depends on:** T0. **Parallel:** with T1, T2, T3, T4.

**Files:**
- Modify: `src/multiuser/owners/devices.go`, `src/multiuser/owners/devices_test.go`, `src/domains/device/device.go` (`Label string \`json:"label,omitempty"\`` with `// WTF:`), `src/multiuser/mcpauth/tools.go` (add `label` to `wtf_my_devices`)
- Create: `src/multiuser/owners/labels.go` (validation), `src/multiuser/web/devices_label.go` (routes, so `devices.go` UI edits stay in T7)

**Interfaces:**
- `owners.NormalizeLabel(s string) (string, error)`: trims; collapses internal whitespace; 1 to 40 runes; refuses control characters and the Unicode bidi override and isolate characters (U+202A to U+202E, U+2066 to U+2069); empty means "clear the name".
- `ScopedDevices.ListDevices` and `GetDevice` fill `Label` from `store.LabelsOf` (one query per call, not per device).
- Routes (registered by T8):
  - `POST /ui/devices/:id/label` (page, CSRF): htmx fragment response; T7 draws it.
  - `PUT /devices/:device_id/label` JSON `{"label": "..."}` behind the API gate (cookie or API key). Check it does not collide with an upstream `/devices/:device_id/...` route (`grep -rn '"/devices/' src/ui/rest`); if it does, use `/wtf/devices/:device_id/label` and note it.
- Audit `device_renamed` (no label text in the audit row).

- [ ] **Step 1: Failing tests.** Behaviour: label table (`"  Work   phone "` → `"Work phone"`; 41 runes refused; `"‮enohp"` refused; `""` clears); `TestListShowsMyLabels`; `TestLabelAnotherUsersDeviceIs404` (both routes); `TestLabelIsEscapedInHTML` (`<script>` label renders escaped on `/`). Shape: `TestDeviceJSONOmitsEmptyLabel` (upstream mode output unchanged).
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Commit.** `git commit -m "feat(multiuser): name your linked numbers"`

---

### Task 7: UI: rename a number

**Kind:** UI. **Model:** sonnet. **Depends on:** T6, T5 (shared `preview_test.go`). **Parallel:** with backend tasks.

**Files:**
- Modify: `src/multiuser/web/devices.go` (only if the label route needs the shared render helpers), `src/multiuser/web/templates/home.html`, `src/multiuser/web/preview_test.go`, `src/multiuser/web/devices_test.go`
- Create: `src/multiuser/web/templates/rename_device.html`

**Design:**
- On My WhatsApp, each device row's title becomes the label when set, with the phone number moved into `.row-sub`; without a label the title stays the phone number (today's behaviour).
- Row actions (B1: at most 3 including the kebab): today's row has Reconnect and Unlink. Rename goes in as a third control only if the row has room; otherwise introduce the row kebab (`{{icon "ellipsis"}} (Lucide `ellipsis`; add the SVG)`, `aria-label="More actions"`, left of the primary button, B2 and B3) holding Rename and Unlink, with Unlink last and red. Check the existing row in `home.html` and pick the one that keeps every row at 3 controls or fewer; say which in the commit.
- Rename opens the shared `sheet` dialog with `rename_device.html`: one `.field` "Name" (placeholder "e.g. Work phone"), Save primary, Cancel, and "Remove name" as a ghost button when a label exists. Errors in `.form-error`. Save swaps the row (`hx-target` the row's id, `hx-swap="outerHTML"`) and closes the dialog.
- Page sub on My WhatsApp gains the retention fact in task words: "Messages are kept for 24 hours." (the number comes from settings; hidden when retention is off).

- [ ] **Step 1: Failing tests.** Behaviour: `TestRenameSwapsTheRow`; `TestRowShowsLabelThenNumber`; `TestRemoveNameRestoresTheNumberTitle`; `TestRetentionLineFollowsTheSetting`. Shape: `TestDeviceRowHasAtMostThreeControls` (scan the rendered row; port the house rule B1 guard if none exists).
- [ ] **Step 2: Implement, fixtures** (row with and without a label, the dialog, its error state, the kebab open) **and screenshots** at 390x844 first.
- [ ] **Step 3: Commit.** `git commit -m "feat(web): rename a linked number"`

---

### Task 8: Wiring, isolation and docs

**Kind:** backend + docs. **Model:** opus. **Depends on:** T1 to T7. **Parallel:** none.

**Files:**
- Modify: `src/multiuser/runtime.go`, `src/cmd/rest.go` (only if start-up needs the chat repo handed in), `src/cmd/multiuser.go`, `src/cmd/multiuser_isolation_test.go`, `readme.md`, `render.yaml`

- [ ] **Step 1: Failing tests.** `TestRetentionHooksInstalledOnlyInMultiUser` (`HistoryCutoffHook` set after `Setup`, nil after `Close`; nil when multi-user is off); `TestPendingDeletionsResumeAtStartup` (a user left `deleting` in the DB is gone after `Setup` + `ResumePending`); isolation: Alice cannot label `d_bob` (page and JSON, cookie and key); Alice's delete never calls `RemoveDevice` for `d_bob`; every new route is gated (the existing walk picks them up; add none to `publicRoutes`).
- [ ] **Step 2: Implement.** In `Setup`: build `retention.Worker{Window, Chats: chatStorageRepo, Accounts: st, FileRoots: ["statics/media", "statics/senditems"]}` and start `Run` with a context cancelled by `Close`; set `whatsapp.HistoryCutoffHook`; build `account.Deleter` (inner device usecase, the OAuth server's store, `serviceID`) and run `ResumePending` once, logging failures; register the label routes and the delete route via `web.Deps`.
- [ ] **Step 3: Docs.** `readme.md`: `WTF_RETENTION_HOURS` row (default 24; what is kept and what is deleted, in two lines). `render.yaml`: `WTF_RETENTION_HOURS: "24"`.
- [ ] **Step 4: Verification.** The captain runs the full suite once (`cd src && go vet ./... && go test ./...`), then `code-review-loop`, then an adversarial review (opus) over T1, T2, T4. On the deployed preview, walk with Aryeh: link a test phone, see history arrive with nothing older than 24 hours, wait for (or force with a one-off admin trigger in a test build only) a retention run, rename the number, delete a test account and confirm the phone shows the companion removed.
- [ ] **Step 5: Commit and push the branch; open the PR.**

---

### Task 9 (optional, build only on Aryeh's yes): a per-user live feed in place of `/ws`

**Kind:** backend + UI. **Model:** opus. **Depends on:** T8.

Phase 1 turned the upstream `/ws` off because its broadcasts carry every device's list. The redesign polls device status every 3 seconds while a link dialog is open, which works. A per-user feed is only worth building if live message display is wanted on a WTF page, which is not in scope today.

If built: `GET /ui/events` (page route, session only) as Server-Sent Events; the server keeps one channel per connected user; `whatsapp.EventHook` publishes `device_state` events (id, state; no message content) only to the owner of the device, looked up with `store.OwnerOf`; heartbeat every 25 seconds; at most 3 connections per user; `htmx` SSE extension vendored (or a 20-line `EventSource` in `app.js`). Tests: `TestEventsGoOnlyToTheOwner` (two users, one device event, one receiver), `TestConnectionLimit`, `TestDisconnectFreesTheSlot` (mechanism). The isolation test adds `GET /ui/events` cases. Remove the 3-second poll only after the feed is proven on Render (whose proxy must keep SSE open; check the `realtime-push-python-on-render` skill's notes on idle timeouts).

---

## Deviations and defaults chosen (tell the user)

1. **The phase split is inferred**, because the design doc could not be read. Phase 3 holds retention, Delete my account and device names; phase 4 holds the waitlist and opening sign-up. If the spec says otherwise, the tasks move between plans unchanged.
2. **Retention covers message content and media only.** Contacts, push names, device sessions and the accounts database stay, because removing them breaks the service. Audit rows are kept 90 days. **Needs Aryeh** to confirm the 24 hours also applies to the superadmin's own numbers and to the service number (default: yes, everyone, no exceptions).
3. **History sync is cut at the window**, so a newly linked phone shows only the last 24 hours. That is the point of retention, but it is visible on day one.
4. **Delete my account asks for the last 4 digits of the number**, not a fresh sign-in. Cheap protection against an accidental click; it does not protect against someone already holding the person's session. A fresh-sign-in requirement can be added later.
5. **The account that owns the service number cannot be deleted** until another number is picked, because deleting it would stop sign-in for everyone.
6. **Device names live in WTF's accounts database, not in WhatsApp**, and appear on My WhatsApp, in `GET /devices` (`label`) and in the `wtf_my_devices` MCP tool. Renaming is not an MCP tool.
7. **The live feed is not built by default** (Task 9 is optional).

## Self-review notes

- Coverage: 24-hour retention (T0, T1, T2, T3, T8); Delete my account (T0, T4, T5, T8); rename a device (T0, T6, T7); per-user live feed (T9, optional).
- Names checked across tasks: `Settings.RetentionHours`/`RetentionWindow`, `store.StatusDeleting`, `(*Store).DeleteUser`/`UsersWithStatus`/`SetLabel`/`LabelsOf`/`PruneExpired`, `PurgeMessagesBefore`/`PurgeResult`, `retention.Worker`/`SweepDir`, `whatsapp.HistoryCutoffHook`, `account.Deleter`/`ErrOwnsServiceNumber`/`ResumePending`, `owners.NormalizeLabel`, `domainDevice.Device.Label`.
- Only T0 appends accounts-store migrations; only T1 appends a chat storage migration; only T8 edits `runtime.go`.
