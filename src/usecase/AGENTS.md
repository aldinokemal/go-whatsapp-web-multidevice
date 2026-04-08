# usecase

Application use cases. Each file bridges one domain to infrastructure. 1:1 mapping with domain packages.

## KEY FILES
- `send.go` (1820 lines) — Largest. Handles all message types (text, image, video, document, sticker, audio, poll, contact, location, link)
- `chat.go` (375 lines) — Chat list, messages, archive, pin, disappearing timer, label
- `group.go` (614 lines) — Group CRUD, participants, settings
- `user.go` (413 lines) — User info, avatar, privacy, contacts
- `message.go` — Message operations (react, delete, revoke, star)
- `app.go` — Login, logout, reconnect
- `device.go` — Multi-device management operations
- `newsletter.go` — Newsletter/channel operations

## CONVENTIONS
- Each usecase struct: `type serviceX struct { repo, whatsmeow deps }`
- Device context: use `deviceIDFromContext(ctx)` to get resolved device ID
- Filter construction: build domain filter struct → pass to repository
- Pagination: use `GetFilteredChatCount(filter)` for accurate totals (not `GetTotalChatCount`)
- After any whatsmeow state change, update local chat storage for consistency

## ANTI-PATTERNS
- Never return stale data after a mutation — sync local DB immediately
- Never use unscoped queries — always pass device_id via filter or context
