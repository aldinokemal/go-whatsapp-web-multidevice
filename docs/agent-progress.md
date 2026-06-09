# Agent Progress

_Recurring `/loop` task: triage open GitHub issues, fix the highest-priority bug, run tests, open a draft PR._

## Run 2 — 2026-06-09 (iteration 2)

Iteration 1 shipped #688 (PR #714, since **merged** to main as `9266165`). This run picked the next actionable bug.

### Selected issue: #675 item A — chat-list name shows blank

> "On the conversation list, the 'name' column should display the phone number when the contact name is empty. Currently this mapping doesn't work, making it hard to identify the sender."

### Root cause

`ListChats` and `GetChatMessages` ([src/usecase/chat.go](../src/usecase/chat.go)) mapped `chatInfo.Name = chat.Name` verbatim. `GetChats` reads the stored name straight from sqlite with no fallback, so a chat persisted before a pushname/group subject is known (or with a stale empty name) returns `"name": ""` to the API and the web UI — the row shows blank.

### Fix

Added `chatDisplayName(jid, name)` — returns the stored name when present, else a JID-derived fallback mirroring the storage-layer convention in `GetChatNameWithPushNameByDevice`: `"Status"` for `status@broadcast`, phone number for 1:1, `Group <id>` / `Newsletter <id>` for those address spaces. Applied in both `ListChats` and `GetChatMessages`.

### Review follow-up (PR #715)

- `kilo-code-bot` flagged that the fallback returned the lowercase local part `"status"` for `status@broadcast` instead of the contracted `"Status"`. **Fixed** in `e0a462e` by adding the `status@broadcast` → `"Status"` special case before the local-part fallback, with test coverage. Replied on the thread and resolved it.

### Verification

- `gofmt` clean; `go build ./...` clean.
- `go test ./...` — all packages pass (post-merge with main).
- `TestChatDisplayName` covers: non-empty passthrough (1:1 + group), empty→phone, empty→`Group <id>`, empty→`Newsletter <id>`, empty→lid local part, empty→`Status`, non-empty `status@broadcast`.

Status: **done** — draft PR [#715](https://github.com/aldinokemal/go-whatsapp-web-multidevice/pull/715), review feedback addressed.

### Note for future iterations

- #674 (Chatwoot 401) turned out to be a **real bug** (whitespace in API token/URL), fixed by the maintainer in PR #713 — not user config as first triaged. Lesson: verify "config" hunches against the code before dismissing.
- The cleanly-fixable bug backlog is now thin; remaining open bugs are WhatsApp/whatsmeow-side or already addressed pending close.
