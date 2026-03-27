# validations

Input validation using `ozzo-validation`. Each domain has a `*_validation.go` + `*_validation_test.go` pair.

## PATTERN
```go
func ValidateXxx(ctx context.Context, request *domain.XxxRequest) error {
    return validation.ValidateStruct(request,
        validation.Field(&request.Field, validation.Required, ...),
    )
}
```

## CONVENTIONS
- Validation functions are standalone (not methods) for easy testing
- Test files use table-driven tests with `testify/assert`
- `send_validation.go` (516 lines) is the largest — validates all message types
- Context parameter is required even if unused (for future device-scoped validation)

## ANTI-PATTERNS
- Never validate in usecase or handler — always in this package
- Never skip writing tests for new validation functions
