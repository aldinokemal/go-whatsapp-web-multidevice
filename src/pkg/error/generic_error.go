package error

import "net/http"

// GenericError represent as the contract of generic error
type GenericError interface {
	Error() string
	ErrCode() string
	StatusCode() int
}

type InternalServerError string

// Error for complying the error interface
func (e InternalServerError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e InternalServerError) ErrCode() string {
	return "INTERNAL_SERVER_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (e InternalServerError) StatusCode() int {
	return http.StatusInternalServerError
}

type ContextError string

// Error for complying the error interface
func (e ContextError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e ContextError) ErrCode() string {
	return "CONTEXT_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (e ContextError) StatusCode() int {
	return http.StatusRequestTimeout
}

type NotFoundError string

func (e NotFoundError) Error() string {
	return string(e)
}

func (e NotFoundError) ErrCode() string {
	return "NOT_FOUND"
}

func (e NotFoundError) StatusCode() int {
	return http.StatusNotFound
}

type UnauthorizedError string

func (e UnauthorizedError) Error() string {
	return string(e)
}

func (e UnauthorizedError) ErrCode() string {
	return "UNAUTHORIZED"
}

func (e UnauthorizedError) StatusCode() int {
	return http.StatusUnauthorized
}

type BadRequestError string

func (e BadRequestError) Error() string {
	return string(e)
}

func (e BadRequestError) ErrCode() string {
	return "BAD_REQUEST"
}

func (e BadRequestError) StatusCode() int {
	return http.StatusBadRequest
}

type RateLimitError string

func (e RateLimitError) Error() string {
	return string(e)
}

func (e RateLimitError) ErrCode() string {
	return "RATE_LIMIT_EXCEEDED"
}

func (e RateLimitError) StatusCode() int {
	return http.StatusTooManyRequests
}

type ConflictError string

func (e ConflictError) Error() string {
	return string(e)
}

func (e ConflictError) ErrCode() string {
	return "CONFLICT"
}

func (e ConflictError) StatusCode() int {
	return http.StatusConflict
}

// ServiceUnavailableError
type ServiceUnavailableError string

func (e ServiceUnavailableError) Error() string {
	return string(e)
}

func (e ServiceUnavailableError) ErrCode() string {
	return "SERVICE_UNAVAILABLE"
}

func (e ServiceUnavailableError) StatusCode() int {
	return http.StatusServiceUnavailable
}

func NewValidationError(message string) error {
	return ValidationError(message)
}

func NewNotFoundError(message string) error {
	return NotFoundError(message)
}

func NewUnauthorizedError(message string) error {
	return UnauthorizedError(message)
}

func NewInternalServerError(message string) error {
	return InternalServerError(message)
}

func NewBadRequestError(message string) error {
	return BadRequestError(message)
}

func NewRateLimitError(message string) error {
	return RateLimitError(message)
}

func NewConflictError(message string) error {
	return ConflictError(message)
}

func NewServiceUnavailableError(message string) error {
	return ServiceUnavailableError(message)
}
