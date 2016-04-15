package imgserver

import "fmt"

type HandlerError struct {
	statusCode  int
	description string
	cause       error
}

func (e *HandlerError) Error() string {
	if e.cause == nil {
		return e.description
	}
	return fmt.Sprint(e.description, " : ", e.cause.Error())
}

func NewHandlerError(statusCode int, description string) *HandlerError {
	return &HandlerError{statusCode: statusCode, description: description}
}
