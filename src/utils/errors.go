package utils

import "fmt"

func PanicIfNeeded(err any, message ...string) {
	if err != nil {
		if fmt.Sprintf("%s", err) == "record not found" && len(message) > 0 {
			panic(message[0])
		} else {
			panic(err)
		}
	}
}

type ValidationError struct {
	Message string
}

func (validationError ValidationError) Error() string {
	return validationError.Message
}

type AuthError struct {
	Message string
}

func (err AuthError) Error() string {
	return err.Message
}
