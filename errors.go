package imgserver

import  (

)
import "fmt"

type handlerError struct {
	statusCode int
	description string
	cause error
}

func (e * handlerError) Error() string {
	if e.cause == nil {
		return e.description
	}
	return fmt.Sprint(e.description, " : ", e.cause.Error())
}
