# Domain contracts

These packages define request/response DTOs and usecase/storage interfaces.
Validation, SQL, transport parsing, and WhatsApp operations belong in other layers.

- Send DTOs live in `send/`; retain `BaseRequest` where chat/contact targeting uses
  it. `send/interfaces.go` composes the smaller sender interfaces into `ISendUsecase`.
- Preserve JSON and form tags across REST, MCP, views, docs, and tests. Optional
  `ReplyMessageID *string` remains `reply_message_id`; boolean filters use `*bool`
  when omitted and `false` have different meanings.
- Existing multipart and whatsmeow types may remain in contracts that need them;
  do not spread protocol dependencies into unrelated DTOs.
- `chatstorage/chatstorage.go` holds storage entities, including Chatwoot links and
  retry events. Preserve `DeviceID` throughout message, chat, and edit flows; these
  Chatwoot entities are persistence contracts, not REST API payloads.
- For repository changes, use `chatstorage/interfaces.go` and the linked
  [storage guide](../infrastructure/chatstorage/AGENTS.md). User/device lookups use
  `GetMessageByIDAndDevice`; chat-specific lookups can use `GetMessageByIDChatAndDevice`.
  Global methods need an explicitly global caller contract.
