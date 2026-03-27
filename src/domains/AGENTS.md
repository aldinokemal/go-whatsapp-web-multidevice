# domains

Business domain layer. Each subdomain has `interfaces.go` (usecase interface) + request/response structs.

## STRUCTURE
```
domains/
├── app/         # App lifecycle (login, logout, reconnect)
├── chat/        # Chat operations (list, messages, archive, pin, label)
├── chatstorage/ # Chat/message storage entities + IChatStorageRepository interface
├── device/      # Multi-device management
├── group/       # Group CRUD + participants
├── message/     # Message operations (react, delete, revoke, star)
├── newsletter/  # Newsletter/channel operations
├── send/        # Message sending (text, image, video, document, etc.)
├── settings/    # Application settings
└── user/        # User info, avatar, privacy, contacts
```

## CONVENTIONS
- Each domain package exports: `interfaces.go` (usecase interface), request/response structs
- Use `*bool` for optional boolean filters (e.g., `Archived *bool` in `ListChatsRequest`)
- Request structs validate via `ozzo-validation` in `validations/` package
- `chatstorage/` is the only domain with a repository interface — others use whatsmeow directly

## ANTI-PATTERNS
- Never put business logic in domain packages — they define contracts only
- Never import infrastructure packages from domains
