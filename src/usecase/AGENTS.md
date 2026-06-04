# USECASE

Generated: 2026-06-05

## OVERVIEW

Usecases orchestrate validation, device/client lookup, WhatsApp operations, storage, and response mapping.

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Send message/media | `send.go` | Largest file; handles text, media, links, contacts, polls, presence, quote context, and stored sent messages. |
| Quoted replies | `send.go` `mergeReplyContext` | Uses `GetMessageByIDAndDevice(deviceIDFromContext(ctx), id)` before setting `ContextInfo`. |
| Chat listing/history | `chat.go` | Device ID comes from context and must reach chat/message filters. |
| Message actions | `message.go` | Reaction/revoke may use global ID lookup to resolve sender for WhatsApp protocol behavior. |
| Group operations | `group.go` | Handles participant JID conversion, names, invite links, and group settings. |
| Account/user operations | `user.go` | Uses current device client and chat storage. |
| Device operations | `device.go` | Delegates to `whatsapp.DeviceManager`. |

## CONVENTIONS

- Constructor names are `New*Service` and return the matching domain interface.
- Validate before doing network/storage work: `validations.Validate*`.
- Resolve WhatsApp clients from context with `whatsapp.ClientFromContext(ctx)`.
- For chat history, derive device scope with `deviceIDFromContext(ctx)` and pass it to repository filters.
- For send operations, sanitize/validate phone/JID, build whatsmeow payloads, send, then store sent messages with context when appropriate.
- `wrapSendMessage` uses `whatsapp.SendMessageWithReachoutRetry`; keep 463 normalization in this layer aligned with infrastructure retry behavior.
- Preserve existing error style: package errors from `pkg/error`, wrapped validation errors, and direct `fmt.Errorf` for contextual failures.

## ANTI-PATTERNS

- Do not fall back to the global client when a request has an explicit device context.
- Do not skip validation because the REST handler already parsed the body.
- Do not return Fiber/MCP response objects from usecases.
- Do not perform unscoped chat/message reads from this layer unless the operation documents why global ID lookup is protocol-safe.
- Do not replace device-scoped reply lookup with `GetMessageByID`; quoted replies must not bind a message from another device.

## TESTING

- Existing usecase tests are narrow. Add focused tests when changing mapping, MIME/media decisions, reply context, or device-scoped chat behavior.
- Stubs live beside tests rather than using gomock/mockery.
