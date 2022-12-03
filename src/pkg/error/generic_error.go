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
