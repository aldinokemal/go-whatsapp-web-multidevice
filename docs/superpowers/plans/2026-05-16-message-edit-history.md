# Message Edit History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist WhatsApp message edits in the database while keeping the latest message content updated and storing a full edit history.

**Architecture:** Keep `messages` as the current state of each message, keyed by the original WhatsApp message ID. Add an append-only `message_edits` table that records every edit event with the original message ID, edit event ID, previous content, new content, editor identity, and timestamp. The WhatsApp event handler continues to call the storage repository once; the repository detects `MESSAGE_EDIT`, updates the original message row, and records the edit history atomically.

**Tech Stack:** Go, `database/sql`, SQLite migrations, existing `whatsmeow` event models, `testify`/standard `testing`.

---

### Task 1: Add edit history domain model and persistence helpers

**Files:**
- Modify: `src/domains/chatstorage/chatstorage.go`
- Modify: `src/infrastructure/chatstorage/sqlite_repository.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCreateMessageStoresEditHistoryAndUpdatesOriginalMessage(t *testing.T) {
    // Arrange a repository with one stored message and an edit event that
    // changes the body from "hello" to "hello again".

    // Expect:
    // - messages row keeps the same ID
    // - messages.content becomes "hello again"
    // - message_edits gets one appended row with previous_content = "hello"
    //   and new_content = "hello again"
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src && go test ./infrastructure/chatstorage -run TestCreateMessageStoresEditHistoryAndUpdatesOriginalMessage -v`
Expected: FAIL because edit history persistence does not exist yet.

- [ ] **Step 3: Implement minimal schema and storage types**

```go
type MessageEdit struct {
    ID                string
    OriginalMessageID string
    EditEventID       string
    ChatJID           string
    DeviceID          string
    Editor            string
    PreviousContent   string
    NewContent        string
    EditedAt          time.Time
    CreatedAt         time.Time
}
```

Add a `message_edits` table migration and a helper that inserts edit rows.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd src && go test ./infrastructure/chatstorage -run TestCreateMessageStoresEditHistoryAndUpdatesOriginalMessage -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/domains/chatstorage/chatstorage.go src/infrastructure/chatstorage/sqlite_repository.go src/infrastructure/chatstorage/sqlite_repository_edit_test.go
git commit -m "feat: persist whatsapp message edit history"
```

### Task 2: Keep webhook behavior and storage updates aligned for edit events

**Files:**
- Modify: `src/infrastructure/whatsapp/event_message_handler.go`
- Modify: `src/pkg/utils/whatsapp.go`
- Modify: `src/infrastructure/whatsapp/event_message.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBuildEventPayloadMessageEditIncludesOriginalMessageIDAndBody(t *testing.T) {
    // Build a ProtocolMessage MESSAGE_EDIT payload and assert the webhook
    // payload contains event = "message.edited", original_message_id, and body.
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd src && go test ./infrastructure/whatsapp -run TestBuildEventPayloadMessageEditIncludesOriginalMessageIDAndBody -v`
Expected: FAIL if the edit-specific payload is incomplete.

- [ ] **Step 3: Implement minimal changes**

Ensure `BuildEventMessage` and the edit branch preserve the edited body, and keep the handler passing the event through to both storage and webhook without special-casing it away.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd src && go test ./infrastructure/whatsapp -run TestBuildEventPayloadMessageEditIncludesOriginalMessageIDAndBody -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/infrastructure/whatsapp/event_message_handler.go src/pkg/utils/whatsapp.go src/infrastructure/whatsapp/event_message.go src/infrastructure/whatsapp/event_message_test.go
git commit -m "test: cover whatsapp message edit payloads"
```

### Task 3: Verify repository-wide regressions

**Files:**
- None

- [ ] **Step 1: Run the focused package tests**

Run: `cd src && go test ./infrastructure/chatstorage ./infrastructure/whatsapp`
Expected: PASS.

- [ ] **Step 2: Run the full test suite**

Run: `cd src && go test ./...`
Expected: PASS.

- [ ] **Step 3: Commit verification notes if needed**

```bash
git status --short
```

Expected: only the intended feature files are modified.
