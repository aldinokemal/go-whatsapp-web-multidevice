# DOMAINS

Generated: 2026-06-05

## OVERVIEW

Domain packages hold request/response DTOs and interfaces for usecases and storage. They are contracts, not execution layers.

## STRUCTURE

```text
domains/
|-- app/ chat/ device/ group/ message/ newsletter/ user/
|-- send/          # one file per send request type plus combined sender interface
`-- chatstorage/   # chat/message/edit/device storage entities, filters, repository interface
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add send request | `send/<type>.go`, `send/interfaces.go` | Keep `BaseRequest` embedding if the request targets a chat/contact. |
| Add reply field | `send/text.go`, media request DTOs | Optional `ReplyMessageID *string` serializes as `reply_message_id`. |
| Add usecase contract | `<domain>/interfaces.go` | Return the domain interface from `usecase.New*Service`. |
| Add chat storage method | `chatstorage/interfaces.go` | Also update concrete repository and WhatsApp storage wrapper. |
| Add response field | Matching DTO file | Check REST/MCP serialization expectations before renaming JSON fields. |

## CONVENTIONS

- DTOs use JSON tags for API payloads and may include form/multipart fields where existing request types already do.
- Send requests are split by message type, but `ISendUsecase` composes smaller sender interfaces for compatibility.
- Chat filters use pointer booleans, for example `*bool`, when "not set" differs from `false`.
- Storage entities carry `DeviceID`; preserve it through chat/message/edit flows.
- `GetMessageByIDAndDevice` is the device-scoped ID lookup for user/device-isolated flows.
- Existing contracts expose whatsmeow types in places. Keep that local to contracts that already need protocol details.

## ANTI-PATTERNS

- Do not put validation rules, SQL, Fiber handlers, MCP tool parsing, or whatsmeow send logic in domain packages.
- Do not add a chat/message repository method that cannot be scoped by device unless the caller contract is explicitly global.
- Do not rename JSON fields casually; views, REST clients, MCP tools, docs, and tests may rely on them.
