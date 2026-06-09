# Agent Progress

_Recurring `/loop` task: triage open GitHub issues, fix the highest-priority bug, run tests, open a draft PR._

## Run 3 — 2026-06-09 (iteration 3)

### Bug backlog status

Re-triaged all open issues. The actionable **bug** backlog is exhausted:
- #688 (contact name) and #674 (Chatwoot 401 — real whitespace-in-token bug) are **closed/merged** (#714, #713).
- #675 (chat-list name) is in review (PR #715, feedback addressed).
- Every other open bug is WhatsApp-side (#708/#691 463), upstream whatsmeow (#620/#545), intended behavior (#644), a usage question (#543), or already addressed (#580).

Per the loop's priority list (bugs → regressions → **features**), moved to the most actionable feature request.

### Selected issue: #578 — add session_id to webhooks and JID to API responses

Chosen over #684 (unread flag — already claimed and blocked on whatsmeow not exposing the data). #578 is untriaged, backward-compatible, and the data is already available internally (`DeviceInstance.ID()` = session id, `JID()` = JID).

> Multi-tenant correlation: webhooks carry `device_id` = JID; API responses carry the session id but not the JID. Nothing gives both, so events can't be mapped back to the registered session.

### Implementation

**Part 1 — `session_id` in all webhook payloads.** All 12 event handlers funnel through one chokepoint, `forwardPayloadToConfiguredWebhooks` ([src/infrastructure/whatsapp/webhook_forward.go](src/infrastructure/whatsapp/webhook_forward.go)). Added a single central injection (`addWebhookSessionID`) that resolves the session id from the payload's `device_id` (JID) via `DeviceManager.getDeviceByJID`. Injected synchronously before the Chatwoot goroutine to avoid racing the shared payload map. Backward-compatible: `device_id` stays the JID; `session_id` is added (omitted when the JID isn't mapped). Behind a `sessionIDForJIDFn` seam for testing.

**Part 2 — `jid` in API responses.** Added `JID` to `DevicesResponse` ([domains/app/app.go](src/domains/app/app.go)), populated in `FetchDevices` ([usecase/app.go](src/usecase/app.go)), and added `jid` to `GET /app/status` ([ui/rest/app.go](src/ui/rest/app.go)).

**Docs.** Updated [docs/openapi.yaml](docs/openapi.yaml) (`/app/devices`, `/app/status`) and [docs/webhook-payload.md](docs/webhook-payload.md) (top-level `session_id`).

### Verification

- `gofmt` clean; `go build ./...` clean; OpenAPI YAML validated.
- `go test ./...` — all packages pass.
- New tests: `TestAddWebhookSessionID` (inject / unmapped / no-overwrite), `TestSessionIDForJIDEmpty`, `TestForwardPayloadInjectsSessionID` (end-to-end through the forward path).

Status: **done** — draft PR opened (branch `feat/session-id-and-jid-578`).
