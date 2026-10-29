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

type WaCliError string

// Error for complying the error interface
func (e WaCliError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e WaCliError) ErrCode() string {
	return "INVALID_WA_CLI"
}

// StatusCode will return the HTTP status code based on the error data type
func (e WaCliError) StatusCode() int {
	return http.StatusInternalServerError
}

type WaUploadMediaError string

// Error for complying the error interface
func (e WaUploadMediaError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e WaUploadMediaError) ErrCode() string {
	return "UPLOAD_MEDIA_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (e WaUploadMediaError) StatusCode() int {
	return http.StatusInternalServerError
}

type WaReachoutTimelockError string

// Error for complying the error interface
func (e WaReachoutTimelockError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e WaReachoutTimelockError) ErrCode() string {
	return "WA_REACHOUT_TIMELOCK"
}

// StatusCode will return the HTTP status code based on the error data type
func (e WaReachoutTimelockError) StatusCode() int {
	return http.StatusTooManyRequests
}

const (
	ErrUserNotRegistered  = InvalidJID("user is not registered")
	ErrWaCLI              = WaCliError("your WhatsApp CLI is invalid or empty")
	ErrWaReachoutTimelock = WaReachoutTimelockError("WhatsApp rejected this send with error 463 (reach-out timelock). This is WhatsApp's server-side anti-spam restriction on starting new chats and cannot be bypassed by the API. It usually means there is no prior conversation with this recipient, or the sending account is temporarily restricted from reaching new contacts. To get through: ask the recipient to message you first, reply within an already-established conversation, or open and send the chat once from the phone linked to this session, then reduce your sending rate and wait for the restriction to clear. Newly-linked or low-activity numbers are affected most.")
)
