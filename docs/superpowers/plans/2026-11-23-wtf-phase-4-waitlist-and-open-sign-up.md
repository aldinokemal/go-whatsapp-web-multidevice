# WTF Phase 4: Waitlist, Capacity and Opening Sign-Up Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Each task names its kind, model, files, interfaces, layered tests and dependencies, so it can be cut straight into a builder brief (`captain-and-crew/brief-template.md`).

**Goal:** Let people who are not invited ask for an account and get one when there is room, without the server running out of memory: a waitlist fed by the service number, an admin page to let people in, a server-wide cap on linked numbers (set from the phase 1 memory measurement), and a user list that still works with hundreds of accounts.

**Architecture:** The sign-in handler (`src/multiuser/signin/handler.go`) already answers unknown numbers with "isn't open for sign-ups yet". It gains one branch: record the number on a `waitlist` table and say so. Admins see the waitlist at `/admin/waitlist`; "Let in" creates the account (the same store call "Add user" uses) and sends the sign-in link from the service number. The per-user device limit (`users.max_devices`, enforced atomically by `store.ReserveOwner`) gains a server-wide limit in the same SQL statement. With `WTF_SIGNUP_OPEN=true`, a new number gets an account at once while there is room and joins the waitlist when the server is full. The admin user list moves to paged, searched queries (`{total, items}`, 50 a page).

**Tech Stack:** as phases 1 to 3.

**Spec:** "GOWA multi-user fork: design", https://claude.ai/code/artifact/24e3a973-7165-4898-a922-a8edb24b9824. **Not readable when this plan was written (2026-11-23).** Grounding: the phase 1 plan names the waitlist among phase 2 to 4 items, says phase 4 sets the phone cap from the memory measurement (Task 14 Step 6), and `store.ListUsers` carries "phase 4 adds paging when sign-up opens". Builders who can open the spec read its phase 4 section first and report differences to the captain.

**Skills every builder loads first:** superpowers:test-driven-development with `layering-tests-by-what-they-check`; `building-multitenant-saas-auth` (list endpoints: `{total, items}`, no silent caps, sort and filter in the query); `securing-multitenant-saas` (rate limits, outbound messages); `stress-testing-concurrency-ceilings` for Task 2; for UI tasks `frontend-house-style`, `ui-ux-checklist` and `docs/ui.md`.

**Depends on:** phase 3 merged (account deletion removes the waitlist row: Task 0 here fills the `// phase 4:` note phase 3 left), and the admin "Add user" change from the UI redesign merged (Task 4 reuses its create-and-link path and its `admin_users.html`).

## Global Constraints

- Everything in the earlier phases' Global Constraints still holds.
- **Never create an account under a LID** (phase 1 Review Focus 2) and never put a LID on the waitlist.
- **The waitlist is not an account.** A waitlisted number cannot sign in, has no session, and is not counted against capacity.
- **Outbound WhatsApp from the service number stays rare and human-paced.** The service number is an unofficial client; bulk sends risk a WhatsApp ban that would end sign-in for everyone. "Let in" sends one message per click; there is no "let everyone in" button; at most 20 admit messages per hour server-wide (refused with a clear admin message beyond that).
- **Capacity numbers come from one setting each:** `WTF_MAX_PHONES` (server-wide linked numbers; `0` = no cap, refused at load when `WTF_SIGNUP_OPEN=true`) and `WTF_DEFAULT_MAX_DEVICES` (per new user; default `2`, today's column default). Admins can change one user's limit.
- **Every list** answers `{total, items}` with `total` the number of matches, 50 rows a page, filter, sort and page in SQL, with the filters kept across pages (house rule T3). Sort keys are an allowlist.
- Migrations: accounts-store migrations are appended in Task 0 only.

## Review Focus

1. **Two people linking the last free slot at once.** The server-wide cap and the per-user limit hold under concurrency: one link succeeds, the other is refused, neither half-creates a device. Pinned in Task 2 (mechanism test).
2. **A waitlist that can be used to spam the service number.** One reply per number per hour (the existing limiter) and a global budget; repeated messages never create duplicate rows; the reply text never echoes anything the sender wrote. Pinned in Task 1.
3. **"Let in" for a number that signed up meanwhile, or was let in by another admin in the next tab.** Idempotent: no second account, at most one message. Pinned in Task 3.
4. **Opening sign-up while full.** `WTF_SIGNUP_OPEN=true` with the cap reached puts new numbers on the waitlist instead of creating accounts that can link nothing. Pinned in Task 1.
5. **The user list search as a data leak.** Admin-only (404 to others), searches phone digits only, and the sort keys are an allowlist. Pinned in Task 5.

---

## File Structure

```
src/multiuser/
  config.go                 + MaxPhones, DefaultMaxDevices (Task 0)
  store/
    store.go                + migrations: waitlist (Task 0)
    waitlist.go             Join, List(page), Admit, Decline, Remove (Task 0)
    users.go                + ListUsersPage, SetMaxDevices, CreateUser uses DefaultMaxDevices (Task 0)
    owners.go               ReserveOwner gains the server-wide cap (Task 2)
    *_test.go
  signin/handler.go         + waitlist branch, capacity check (Task 1)
  owners/devices.go         passes the cap through (Task 2)
  capacity/
    capacity.go             Capacity{Linked, Max, Users, Waiting} (Task 2)
  web/
    admin_waitlist.go       /admin/waitlist routes (Task 3)
    admin.go                paged users, per-user limit edit (Task 5)
    templates/admin_waitlist.html, admin_users.html, user_limit.html, admin_settings.html (capacity card)
    templates/layout.html   + "Waitlist" nav entry with a count pill (Task 3)
    preview_test.go         + fixtures (Tasks 3, 5, 6)
  account/delete.go         + remove the waitlist row (Task 0, fills the phase 3 note)
  runtime.go                wiring (Task 7 only)
src/cmd/multiuser_isolation_test.go   + admin-only routes refuse members (Task 7)
readme.md, render.yaml                + WTF_MAX_PHONES, WTF_DEFAULT_MAX_DEVICES (Task 7)
```

---

## Task order and parallelism

```
T0 config + store ──┬─► T1 sign-in waitlist branch ──────────────┐
                    ├─► T2 server-wide cap (store + devices) ────┤
                    ├─► T3 UI: waitlist page ─► T5 UI: users ─► T6 UI: capacity card ─┼─► T7 wiring, isolation, docs ─► T8 open sign-up (with Aryeh)
                    └───────────────────────────────────────────┘
```

| Group | Tasks | Runs in parallel with | Ordering reason |
|---|---|---|---|
| A | T0 | nothing | owns `config.go`, migrations, `store/waitlist.go`, `store/users.go` |
| B | T1, T2 | each other and the UI tasks | T1 owns `signin/`; T2 owns `store/owners.go`, `owners/devices.go`, `capacity/` |
| C | T3, then T5, then T6 | backend tasks | all three edit `layout.html` or `admin_*.html` and `preview_test.go` |
| D | T7 | nothing | `runtime.go`, isolation test, docs |
| E | T8 | nothing | live change on Render; Aryeh's go-ahead |

T1 needs T2's `capacity.Full(ctx)` only through an interface; give T1 a fake and let T7 wire the real one, so T1 and T2 do not wait on each other.

**Sizing:** 8 tasks (T0 to T3 and T5 to T8; T4 was folded into T3). Complexity: T0 M, T1 M, T2 M, T3 M, T5 M, T6 S, T7 M, T8 S (plus the live measurement). Highest risk: T2 (a concurrency ceiling), T1 (a public, unauthenticated entry point that sends messages), T8 (opening to the public on a small instance).

**Models:** opus for T1, T2, T7; sonnet for T0, T3, T5, T6; T8 is the captain with Aryeh.

---

### Task 0: Settings, waitlist table, paged users

**Kind:** backend. **Model:** sonnet. **Depends on:** phase 3 merged. **Parallel:** none.

**Files:**
- Modify: `src/multiuser/config.go`, `src/multiuser/config_test.go`, `src/multiuser/store/store.go`, `src/multiuser/store/users.go`, `src/multiuser/store/users_test.go`, `src/multiuser/account/delete.go`, `src/multiuser/account/delete_test.go`
- Create: `src/multiuser/store/waitlist.go`, `src/multiuser/store/waitlist_test.go`

**Interfaces:**
- `Settings.MaxPhones int` (`WTF_MAX_PHONES`, default `0`; must be > 0 when `SignupOpen`), `Settings.DefaultMaxDevices int` (`WTF_DEFAULT_MAX_DEVICES`, default 2, 1 to 10).
- Migration appended:
  ```sql
  CREATE TABLE waitlist (
      phone TEXT PRIMARY KEY,
      status TEXT NOT NULL DEFAULT 'waiting',   -- waiting | admitted | declined
      created_at TIMESTAMP NOT NULL,
      last_asked_at TIMESTAMP NOT NULL,
      decided_at TIMESTAMP NULL,
      decided_by TEXT NOT NULL DEFAULT '')
  CREATE INDEX waitlist_status_created ON waitlist(status, created_at)
  ```
- `(*Store).JoinWaitlist(ctx, phone) (joined bool, position int, err error)`: insert or bump `last_asked_at`; a `declined` number stays declined (`joined=false`); position = waiting rows created before it, plus one.
- `(*Store).WaitlistPage(ctx, status string, offset, limit int) (total int, items []WaitlistEntry, err error)`; oldest first.
- `(*Store).AdmitFromWaitlist(ctx, phone, by string, maxDevices int) (*User, bool /*createdNow*/, error)`: one transaction: if a user already exists for the phone, mark the row admitted and return it with `createdNow=false`; else create the user and mark admitted.
- `(*Store).DeclineWaitlist(ctx, phone, by) error`; `(*Store).RemoveFromWaitlist(ctx, phone) error`.
- `CreateUser` takes the default device limit from the caller (add `CreateUserWithLimit`; keep `CreateUser` for existing callers, using the column default).
- `(*Store).ListUsersPage(ctx, q UsersQuery) (total int, items []User, err error)` with `UsersQuery{Search string /*digits only*/; Status string /*"", active, disabled*/; Sort string /*allowlist: "created", "last_login", "phone"*/; Desc bool; Offset, Limit int}`; `ListUsers` stays for callers that need all (the admin-link path) but the admin page moves to the paged call in T5.
- `(*Store).SetMaxDevices(ctx, userID string, n int) error` (1 to 10).
- `account.Deleter` step 5: `RemoveFromWaitlist(phone)`.

- [ ] **Step 1: Failing tests.** Behaviour: `TestJoinIsIdempotent` (three joins, one row, `last_asked_at` bumped); `TestDeclinedStaysDeclined`; `TestPositionCountsOnlyWaiting`; `TestAdmitCreatesOnce` (admit twice: one user, `createdNow` true then false); `TestAdmitWhenUserAlreadyExists`; `TestUsersPageTotalsMatches` (`total` equals all matches, not the page length; search `"0100"` finds `+27825550100`; a search with letters is stripped to digits and an empty result says so); `TestUsersSortIsAllowlisted` (`Sort: "id; DROP"` falls back to `created`); `TestDeleteAccountRemovesWaitlistRow`. Mechanism: `TestConcurrentAdmitsCreateOneUser` (10 goroutines admitting one phone: one user row).
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Commit.** `git commit -m "feat(store): waitlist and paged users"`

---

### Task 1: The service number puts people on the waitlist

**Kind:** backend (public entry point). **Model:** opus. **Depends on:** T0. **Parallel:** with T2, T3.

**Files:**
- Modify: `src/multiuser/signin/handler.go`, `src/multiuser/signin/handler_test.go`, `src/multiuser/signin/hardening_test.go`

**Interfaces:**
- `Handler` gains `Waitlist interface{ JoinWaitlist(ctx, phone string) (bool, int, error) }`, `Full func(ctx context.Context) bool` (server at capacity) and `DefaultMaxDevices int`.
- For an unknown, readable phone number (after the LID check and the limiter, unchanged):
  - superadmin: create the account as today;
  - `SignupOpen && !Full(ctx)`: create the account (with `DefaultMaxDevices`) and send the link, as today;
  - otherwise: `JoinWaitlist`; reply once: "You're on the WTF waitlist (number N). We'll message you here when there's room." (`ProductName`, position only; never echo the sender's text); a declined number gets the same "isn't open for sign-ups yet" reply as today, no position.
- Audit `waitlist_joined` (user id empty, no phone in the audit row; the waitlist table already holds the number).
- Known users, disabled users and the token path are unchanged.

- [ ] **Step 1: Failing tests (fake sender, fake waitlist, fake `Full`).** Behaviour: `TestClosedSignupJoinsTheWaitlist`; `TestOpenSignupCreatesWhileThereIsRoom`; `TestOpenSignupWhenFullJoinsTheWaitlist` (Review Focus 4); `TestSuperadminNeverWaitlisted`; `TestDeclinedGetsTheClosedReply`; `TestWaitlistReplyNeverEchoesTheMessage` (send a message containing a URL and a name; the reply contains neither). Security: `TestLIDIsNeverWaitlisted`; `TestRepeatedMessagesReplyOnce` (five messages in an hour from one number: one reply, one row; uses the existing limiter, prove it by setting the limiter to allow all and seeing the test go red).
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Commit.** `git commit -m "feat(signin): waitlist through the service number"`

---

### Task 2: A server-wide cap on linked numbers

**Kind:** backend (concurrency ceiling). **Model:** opus. **Depends on:** T0. **Parallel:** with T1, T3.

**Files:**
- Modify: `src/multiuser/store/owners.go`, `src/multiuser/store/owners_test.go`, `src/multiuser/owners/devices.go`, `src/multiuser/owners/devices_test.go`
- Create: `src/multiuser/capacity/capacity.go`, `src/multiuser/capacity/capacity_test.go`

**Interfaces:**
- `ReserveOwner(ctx, deviceID, userID string, maxPerUser, maxTotal int) (Reservation, error)` where `Reservation` is `Reserved`, `UserLimit` or `ServerFull`. One statement:
  ```sql
  INSERT INTO device_owners (device_id, user_id, created_at, label)
  SELECT ?, ?, ?, ''
  WHERE (SELECT COUNT(*) FROM device_owners WHERE user_id = ?) < ?
    AND (? = 0 OR (SELECT COUNT(*) FROM device_owners) < ?)
  ```
  On zero rows, a second read decides which limit was hit, for the message only (the decision is already made atomically). Superadmins are not exempt from the server cap (the memory is real either way) but are exempt from the per-user limit, as today if that is today's behaviour; check `AddDevice` and keep it.
- `ScopedDevices` gains `MaxTotal func() int`; `AddDevice` returns `ErrServerFull` (a `ValidationError`: "There's no room for another number right now. Try again later.") or the existing `ErrDeviceLimitReached`.
- `capacity.Service{Store; MaxPhones int}` with `Snapshot(ctx) (Capacity, error)` (`Linked`, `Max`, `Users`, `Waiting`) and `Full(ctx) bool` (`Max > 0 && Linked >= Max`). Used by T1 (through `Handler.Full`), T6 and T7.
- Devices that exist but are not yet linked (slot reserved, QR not scanned) count against capacity, because they hold a reservation; the existing rollback on abandoned links (phase 1) keeps them from leaking. Check how long an abandoned slot lives today and say so in the commit.

- [ ] **Step 1: Failing tests.** Behaviour: `TestServerCapRefusesTheNextLink`; `TestUserLimitStillApplies`; `TestCapZeroMeansNoCap`; `TestRemovingANumberFreesASlot`. Mechanism (`stress-testing-concurrency-ceilings`): `TestLastSlotUnderConcurrency` (cap 5, 4 linked, 20 goroutines from 20 users each `AddDevice` at once against a file DB: exactly 1 succeeds, 19 get `ErrServerFull`, `device_owners` has 5 rows, and the inner usecase fake created exactly 1 slot). Prove it can fail: split the count and the insert into two statements in a test-only variant and watch it over-admit.
- [ ] **Step 2: Implement.**
- [ ] **Step 3: Commit.** `git commit -m "feat(multiuser): server-wide cap on linked numbers"`

---

### Task 3: UI: the Waitlist page

**Kind:** UI (+ its routes). **Model:** sonnet. **Depends on:** T0; "Add user" merged. **Parallel:** with T1, T2. **Sequential with:** T5, T6 (shared templates and fixtures).

**Files:**
- Create: `src/multiuser/web/admin_waitlist.go`, `src/multiuser/web/admin_waitlist_test.go`, `src/multiuser/web/templates/admin_waitlist.html`
- Modify: `src/multiuser/web/web.go` (`Deps.Waitlist`, `Deps.SendLink func(ctx, phone, link string) error`, `Deps.Capacity`; and the `Mount` call for the new file), `src/multiuser/web/templates/layout.html` (nav entry), `src/multiuser/web/preview_test.go`
- Add Lucide icons if missing: `list-ordered` (or `hourglass`), `user-plus` (the redesign adds it), `send`.

**Design:**
- Admin nav gains "Waitlist" (`{{icon "hourglass"}}`) between Users and Settings, with a count pill when anyone is waiting (derived at render time, T2 house rule).
- `/admin/waitlist` (`RequireSuperadmin`, 404 to others): `.page-head` "Waitlist", sub "People who asked for an account." A switcher (B4) "Waiting / Declined" (buttons on desktop, one select on a phone). A capacity line in `.page-sub` when a cap is set: "12 of 20 numbers linked".
- `.table` > `.trow` rows (the admin-list component), 50 a page with the pager and "N waiting" count (T3, T12: the figure equals the list): phone (monospace), "Asked 3 days ago", "Asked again 2 hours ago" when bumped. Actions per row: "Let in" (`btn-primary btn-sm`, `{{icon "user-plus"}}`) and "Decline" (`btn-secondary btn-sm btn-quiet-danger`) with a `confirm` ("Decline this number?" / "They won't be told. They can't join the waitlist again unless you let them in from Users.").
- "Let in" posts (CSRF, busy state at once, B7), creates the account via `AdmitFromWaitlist` with `DefaultMaxDevices`, mints a link (`signinLinkFor` from the Add user change) and sends it from the service number: "You're in. Here's your WTF sign-in link. It works once, for 15 minutes: <link>". The row swaps to a done state "Let in. Link sent." with a Copy-link fallback when sending failed ("Couldn't send on WhatsApp. Copy the link and send it yourself." plus the link in a read-only input with Copy).
- When the server is full, "Let in" is hidden (V1) and the page shows a `.notice`: "The server is full. Free a number or raise the limit before letting anyone in."
- Over the hourly admit budget: the row answers "Too many messages this hour. Try again later." without creating anything.
- Empty state: "Nobody is waiting." (`.empty`, `{{icon "hourglass"}}`).
- Declined tab rows offer "Move back to waiting" (no message sent).

- [ ] **Step 1: Failing tests.** Behaviour: `TestLetInCreatesSendsAndSwapsTheRow` (fake sender records one message to the right number; the row fragment says sent); `TestLetInTwiceSendsOnce` (Review Focus 3: second click finds the user and sends nothing); `TestLetInWhenSendFailsShowsTheLink`; `TestLetInHiddenWhenFull`; `TestAdmitBudget` (21st admit in an hour is refused, frozen clock); `TestCountPillEqualsWaitingTotal`. Security: `TestWaitlistIs404ForMembers` (every route); `TestLetInNeedsCSRF`.
- [ ] **Step 2: Implement; preview fixtures** (empty, waiting with a pager, declined tab, row after let-in, send-failed row, server-full notice, phone-width switcher) **and screenshots** at 390x844 first.
- [ ] **Step 3: Commit.** `git commit -m "feat(web): admin waitlist"`

---

### Task 5: UI: Users at scale and per-user limits

**Kind:** UI (+ route changes). **Model:** sonnet. **Depends on:** T0, T3 (shared files). **Parallel:** with backend tasks.

(Task numbering skips 4: "Let in" was folded into Task 3.)

**Files:**
- Modify: `src/multiuser/web/admin.go`, `src/multiuser/web/admin_test.go`, `src/multiuser/web/templates/admin_users.html`, `src/multiuser/web/preview_test.go`
- Create: `src/multiuser/web/templates/user_limit.html`

**Design:**
- `/admin/users` moves to `ListUsersPage`: a type-ahead search box (T8: debounced, from 2 digits, stale requests cancelled, `hx-sync="this:replace"`, focus kept, Enter works) over phone digits; a status filter (All / Active / Disabled) as a switcher; sortable headers for Created, Last sign-in and Phone (T7; ascending then descending; active column marked); 50 a page; "N users" equal to the matches; filters kept in the pager links and the URL.
- Each row gains the number of linked numbers as "1 of 2 numbers"; a row action "Number limit" (inside the row's kebab, which the row now needs to stay at 3 controls, B1) opens the `sheet` dialog `user_limit.html`: a select 1 to 10, Save. Lowering below the current count is allowed and only stops new links; the dialog says "They can keep the numbers they have."
- The "Add user" button from the redesign stays top-right (F1).

- [ ] **Step 1: Failing tests.** Behaviour: `TestUsersPageIsPaged` (120 users: 50 rows drawn, "120 users", page 3 has 20); `TestSearchKeepsFilters`; `TestSortHeadersToggle`; `TestLimitDialogSaves`; `TestLimitOutOfRangeRefused`. Shape: `TestNoAdminListDrawsTheWholeTable` (port the house guard: render with 120 users, count rows ≤ 50). Security: `TestSortKeyIsAllowlisted` (a crafted `sort` query param falls back and does not error).
- [ ] **Step 2: Implement; fixtures** (many users with a pager, search with no result, limit dialog) **and screenshots.**
- [ ] **Step 3: Commit.** `git commit -m "feat(web): paged admin users and number limits"`

---

### Task 6: UI: capacity on Settings

**Kind:** UI. **Model:** sonnet. **Depends on:** T2 (capacity service), T5 (shared fixtures). **Parallel:** with backend tasks.

**Files:**
- Modify: `src/multiuser/web/admin.go` (settings handler only), `src/multiuser/web/templates/admin_settings.html`, `src/multiuser/web/preview_test.go`, `src/multiuser/web/admin_test.go`

**Design:** `/admin/settings` already shows memory (`OwnedDeviceCount` at `admin.go:196`). Add a `.stats` row: "Numbers linked" (`12 of 20`, warn style at 90% and above), "Users", "Waiting" (each tile a link to the list it counts, T10: Users → `/admin/users`, Waiting → `/admin/waitlist`), next to the existing memory figure. When no cap is set, the first tile reads "12 linked" with no "of".

- [ ] **Step 1: Failing tests.** `TestCapacityTilesLinkToTheirLists`; `TestCapacityWarnsNearFull`; `TestTileFiguresEqualTheLists` (the Waiting tile's number equals `WaitlistPage(...).total`).
- [ ] **Step 2: Implement; fixtures; screenshots.**
- [ ] **Step 3: Commit.** `git commit -m "feat(web): capacity tiles on settings"`

---

### Task 7: Wiring, isolation and docs

**Kind:** backend + docs. **Model:** opus. **Depends on:** T1 to T6. **Parallel:** none.

**Files:**
- Modify: `src/multiuser/runtime.go`, `src/cmd/multiuser_isolation_test.go`, `readme.md`, `render.yaml`

- [ ] **Step 1: Failing tests.** `TestSigninHandlerUsesTheRealCapacity` (Setup wires `Handler.Full` to `capacity.Service.Full`); isolation: every `/admin/waitlist*` route answers 404 to a member with a session and to a member's API key; `publicRoutes` unchanged.
- [ ] **Step 2: Implement.** Build `capacity.Service` in `Setup`; set `Handler.Waitlist`, `Handler.Full`, `Handler.DefaultMaxDevices`, `ScopedDevices.MaxTotal`; `web.Deps.SendLink` uses `signin.ClientSender` with the service device (the same path the sign-in reply uses).
- [ ] **Step 3: Docs.** `readme.md`: `WTF_MAX_PHONES`, `WTF_DEFAULT_MAX_DEVICES`, and the waitlist in three lines; replace "Every other user signs in the same way a superadmin invites them to" with the waitlist flow. `render.yaml`: `WTF_MAX_PHONES` with `sync: false` (Aryeh sets the measured value), `WTF_DEFAULT_MAX_DEVICES: "2"`.
- [ ] **Step 4: Verification.** Captain runs the full suite once, `code-review-loop`, and an adversarial review (opus) of T1 and T2.
- [ ] **Step 5: Commit, push the branch, open the PR.**

---

### Task 8: Measure, set the cap, open sign-up (captain with Aryeh)

**Kind:** deploy. **Depends on:** T7 merged and deployed with `WTF_SIGNUP_OPEN=false`.

- [ ] **Step 1: The memory measurement.** If phase 1 Task 14 Step 6 already produced the per-phone table, use it. If not, do it now with Aryeh: link real phones one at a time, read `/admin/settings` memory two minutes after each settles, and set `WTF_MAX_PHONES` to the largest count that stays at or under 70% of the instance's memory (the phase 1 rule: 30% headroom).
- [ ] **Step 2: Decide the instance size with Aryeh** (cost). If the cap on the current plan is too low for the people expected, the next Render plan up raises it; this is his call.
- [ ] **Step 3: Set `WTF_MAX_PHONES` in the Render dashboard** (Aryeh types it), deploy, confirm the Settings tiles.
- [ ] **Step 4: Walk the paths** (`walk-user-paths`): from a phone with no account, message the service number with sign-up closed (waitlisted, position shown); let it in from `/admin/waitlist` (link arrives); fill the cap with a test cap of `1` on a preview and confirm the next link is refused with the "no room" message.
- [ ] **Step 5: Open sign-up only on Aryeh's explicit go-ahead.** Set `WTF_SIGNUP_OPEN=true`. Watch the first day's memory and the waitlist.

---

## Deviations and defaults chosen (tell the user)

1. **The phase split is inferred** (the design doc could not be read). This phase holds the waitlist, the capacity cap and paged users, because they are what opening sign-up needs.
2. **"Let in" is one person per click, with a 20-an-hour ceiling**, to keep the service number from looking like a bulk sender to WhatsApp. No bulk admit.
3. **Declined people are not told.** A declined number gets the generic "isn't open for sign-ups yet" reply if it asks again.
4. **With sign-up open, accounts are created only while there is room**; when full, new people join the waitlist and are let in by an admin. Automatic admission when a slot frees up is not built.
5. **Unlinked but reserved slots count against capacity**, because the memory cost starts at link time and the reservation must be honoured.
6. **The "Add user" redesign change is a prerequisite**, because Let in reuses its account-plus-link path.

## Self-review notes

- Coverage: waitlist (T0, T1, T3, T7); capacity cap from the memory measurement (T2, T6, T8); paging when sign-up opens (T0, T5); opening sign-up (T8).
- Names checked across tasks: `Settings.MaxPhones`/`DefaultMaxDevices`, `(*Store).JoinWaitlist`/`WaitlistPage`/`AdmitFromWaitlist`/`DeclineWaitlist`/`RemoveFromWaitlist`/`ListUsersPage`/`SetMaxDevices`, `UsersQuery`, `ReserveOwner(..., maxTotal)`/`Reservation`, `owners.ErrServerFull`, `capacity.Service`/`Capacity`/`Full`, `signin.Handler.Waitlist`/`Full`/`DefaultMaxDevices`, `web.Deps.Waitlist`/`SendLink`/`Capacity`.
- Only T0 appends accounts-store migrations; only T7 edits `runtime.go`.
