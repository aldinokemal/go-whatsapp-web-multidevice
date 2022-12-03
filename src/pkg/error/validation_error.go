package error

import "net/http"

type ValidationError string

func (err ValidationError) Error() string {
	return string(err)
}

// ErrCode will return the error code based on the error data type
func (err ValidationError) ErrCode() string {
	return "VALIDATION_ERROR"
}

// StatusCode will return the HTTP status code based on the error data type
func (err ValidationError) StatusCode() int {
	return http.StatusBadRequest
}
