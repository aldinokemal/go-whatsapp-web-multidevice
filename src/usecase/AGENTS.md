# USECASE

Generated: 2026-05-24

## OVERVIEW

Usecases orchestrate validation, device/client lookup, WhatsApp operations, storage, and response mapping.

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Send message/media | `send.go` | Largest file; handles text, media, links, contacts, polls, presence, and stored sent messages. |
| Chat listing/history | `chat.go` | Device ID comes from context and must reach chat/message filters. |
| Group operations | `group.go` | Handles participant JID conversion, names, invite links, and group settings. |
| Account/user operations | `user.go` | Uses current device client and chat storage. |
| Device operations | `device.go` | Delegates to `whatsapp.DeviceManager`. |

## CONVENTIONS

- Constructor names are `New*Service` and return the matching domain interface.
- Validate before doing network/storage work: `validations.Validate*`.
- Resolve WhatsApp clients from context with `whatsapp.ClientFromContext(ctx)`.
- For chat history, derive device scope with `deviceIDFromContext(ctx)` and pass it to repository filters.
- For send operations, sanitize/validate phone/JID, build whatsmeow payloads, send, then store sent messages with context when appropriate.
- Preserve existing error style: package errors from `pkg/error`, wrapped validation errors, and direct `fmt.Errorf` for contextual failures.

## ANTI-PATTERNS

- Do not fall back to the global client when a request has an explicit device context.
- Do not skip validation because the REST handler already parsed the body.
- Do not return Fiber/MCP response objects from usecases.
- Do not perform unscoped chat/message reads from this layer.

## TESTING

- Existing usecase tests are narrow. Add focused tests when changing mapping, MIME/media decisions, or device-scoped chat behavior.
- Stubs live beside tests rather than using gomock/mockery.
