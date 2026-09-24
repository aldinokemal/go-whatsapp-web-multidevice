# Request validation

Use `validation.ValidateStructWithContext` and wrap ozzo errors with
`pkgError.ValidationError(err.Error())`. Cross-field and format checks follow
struct validation before the usecase touches WhatsApp, storage, or media helpers.

- `send_validation.go` owns phone, mention, file/URL, MIME, size, duration, and poll
  rules. Require international phone format; local `08...` fails. `@everyone`
  bypasses phone validation.
- Enforce exactly one of file/URL for request types with that contract. Read size
  limits from `config.WhatsappSettingMax*`; keep `reply_message_id` optional.
- `chat_validation.go` also applies pagination defaults. Preserve that mutation
  when changing validators or their callers.
- `group_validation.go` uses whatsmeow participant action constants.
- In `message_validation.go` and other validators, `validation.Required` rejects
  plain `false`. Use a pointer or explicit check when `false` is valid.

Tests are colocated and table-driven. Multipart fixtures can use `multipart.FileHeader`
with a `Content-Type` header. Cover the changed rule's accepted and rejected inputs.
