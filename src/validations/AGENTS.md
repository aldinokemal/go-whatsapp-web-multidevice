# validations

Input validation using `ozzo-validation`. Each domain has a `*_validation.go` + `*_validation_test.go` pair (16 files total).

## PATTERN
```go
func ValidateXxx(ctx context.Context, request *domain.XxxRequest) error {
    return validation.ValidateStruct(request,
        validation.Field(&request.Field, validation.Required, ...),
    )
}
```

## FILE MAP
| Domain | Validation | Test | Notes |
|--------|-----------|------|-------|
| send | send_validation.go (516 lines) | send_validation_test.go (1113 lines) | Largest; also `send_validation_test_sticker.go` |
| group | group_validation.go | group_validation_test.go (708 lines) | Second largest test file |
| chat | chat_validation.go | chat_validation_test.go | |
| message | message_validation.go | message_validation_test.go | |
| user | user_validation.go | user_validation_test.go | |
| app | app_validation.go | app_validation_test.go | |
| newsletter | newsletter_validation.go | newsletter_validation_test.go | |

## CONVENTIONS
- Validation functions are standalone (not methods) for easy testing
- Test files use table-driven tests with `testify/assert`
- Context parameter is required even if unused (for future device-scoped validation)

## ANTI-PATTERNS
- Never validate in usecase or handler — always in this package
- Never skip writing tests for new validation functions
