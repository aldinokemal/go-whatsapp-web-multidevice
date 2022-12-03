package error

import "net/http"

type InvalidJID string

// Error for complying the error interface
func (e InvalidJID) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e InvalidJID) ErrCode() string {
	return "INVALID_JID"
}

// StatusCode will return the HTTP status code based on the error data type
func (e InvalidJID) StatusCode() int {
	return http.StatusBadRequest
}

type WebhookError string

// Error for complying the error interface
func (e WebhookError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e WebhookError) ErrCode() string {
	return "WEBHOOK_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (e WebhookError) StatusCode() int {
	return http.StatusInternalServerError
}

const (
	ErrInvalidJID = InvalidJID("your JID is invalid")
)
