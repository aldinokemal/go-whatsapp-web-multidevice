package error

import "net/http"

type LoginError string

// Error for complying the error interface
func (e LoginError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e LoginError) ErrCode() string {
	return "ALREADY_LOGGED_IN"
}

// StatusCode will return the HTTP status code based on the error data type
func (e LoginError) StatusCode() int {
	return http.StatusBadRequest
}

type ReconnectError string

func throwReconnectError(text string) GenericError {
	return AuthError(text)
}

// Error for complying the error interface
func (e ReconnectError) Error() string {
	return string(e)
}

// ErrCode will return the error code based on the error data type
func (e ReconnectError) ErrCode() string {
	return "RECONNECT_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (e ReconnectError) StatusCode() int {
	return http.StatusBadRequest
}

type AuthError string

func throwAuthError(text string) GenericError {
	return AuthError(text)
}

func (err AuthError) Error() string {
	return string(err)
}

// ErrCode will return the error code based on the error data type
func (err AuthError) ErrCode() string {
	return "AUTHENTICATION_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (err AuthError) StatusCode() int {
	return http.StatusUnauthorized
}

type qrChannelError string

func throwQrChannelError(text string) GenericError {
	return qrChannelError(text)
}

func (err qrChannelError) Error() string {
	return string(err)
}

// ErrCode will return the error code based on the error data type
func (err qrChannelError) ErrCode() string {
	return "QR_CHANNEL_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (err qrChannelError) StatusCode() int {
	return http.StatusInternalServerError
}

type sessionSavedError string

func throwSessionSavedError(text string) GenericError {
	return sessionSavedError(text)
}

func (err sessionSavedError) Error() string {
	return string(err)
}

// ErrCode will return the error code based on the error data type
func (err sessionSavedError) ErrCode() string {
	return "SESSION_SAVED_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (err sessionSavedError) StatusCode() int {
	return http.StatusInternalServerError
}

var (
	ErrAlreadyLoggedIn = LoginError("you are already logged in.")
	ErrNotConnected    = throwAuthError("you are not connect to services server, please reconnect")
	ErrNotLoggedIn     = throwAuthError("you are not logged in")
	ErrReconnect       = throwReconnectError("reconnect error")
	ErrQrChannel       = throwQrChannelError("QR channel error")
	ErrSessionSaved    = throwSessionSavedError("your session have been saved, please wait to connect 2 second and refresh again")
)
