# Usecases

Usecases validate requests, resolve the device/client, perform WhatsApp and storage
operations, and return domain responses. Constructors use `New*Service` and return
the matching domain interface; keep Fiber/MCP response types in the adapters.

- Validate with `validations.Validate*` before network or storage work, even when
  the transport already parsed the request. Use existing `pkg/error` errors and
  contextual wrapping; the package name is `error`, commonly aliased `pkgError`.
- Resolve clients with `whatsapp.ClientFromContext(ctx)` and chat/message scope
  with `deviceIDFromContext(ctx)`. An explicit device context must not fall back to
  the global client. Pass scope through repository filters and sent-message storage.
- `send.go` owns send payloads and quote context. `mergeReplyContext` must use
  `GetMessageByIDAndDevice`; an unscoped lookup can quote another device's message.
- `wrapSendMessage` calls whatsmeow's `SendMessage` and normalizes errors. Code 463
  is surfaced as a server-side reach-out restriction; whatsmeow owns the token lifecycle.
  Async sent-message persistence retains device context while detaching cancellation.
- `schedule.go` owns `ScheduleService` (durable worker, pause/resume/cancel) and
  `scheduledSendService`, a decorator over `ISendUsecase`. A new send method whose
  request embeds `ScheduleOptions` must branch on `IsScheduled()` and get
  `validateScheduledPayload`/`dispatch` cases. The worker dispatches through the
  undecorated base usecase, never the decorator. Rows are keyed by `instance.ID()`
  (slot alias) so the worker can resolve them with `DeviceManager.GetDevice`.
- `chat.go` owns history queries; `message.go` owns reactions, revokes, edits, and
  other message actions. Existing protocol-specific global ID lookups are exceptions,
  not a pattern for new user-facing reads; verify their caller contract before reuse.
- `group.go` handles participant JIDs and group settings, `user.go` account/contact
  operations, and `device.go` delegates lifecycle operations to `DeviceManager`.

For mapping, media, quote, or isolation changes, use the colocated tests and stubs
to cover the affected behavior. Choose test scope from the change rather than
requiring every usecase suite for each edit.
