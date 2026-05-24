# VALIDATIONS

Generated: 2026-05-24

## OVERVIEW

Validation functions enforce request shape before usecases touch whatsmeow, storage, or media helpers.

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Send validation | `send_validation.go` | Phone format, file-vs-URL, MIME, max sizes, duration, mentions, poll options. |
| Chat validation | `chat_validation.go` | Mutates request defaults for limit/offset before validating. |
| Group validation | `group_validation.go` | Uses whatsmeow participant action constants. |
| Message validation | `message_validation.go` | Watch boolean `Required` behavior. |
| Tests | `*_test.go` | Table-driven, expected error equality or substring checks. |

## CONVENTIONS

- Use `validation.ValidateStructWithContext(ctx, &request, validation.Field(...))`.
- Wrap ozzo errors as `pkgError.ValidationError(err.Error())`.
- Add custom checks after ozzo validation for cross-field rules and domain-specific formats.
- Phone numbers must be international format; Indonesian local `08...` should fail.
- `@everyone` is a special mention and bypasses phone validation.
- Multipart tests usually construct `multipart.FileHeader` with a `Content-Type` header directly.

## ANTI-PATTERNS

- Do not use `validation.Required` on a plain `bool` when `false` is valid; use a pointer or explicit validation pattern.
- Do not accept both file and URL for request types that require exactly one input.
- Do not let max-size checks drift from `config.WhatsappSettingMax*`.
