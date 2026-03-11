package error

import "errors"

// Pull out shared errors
var (
	ErrUnauthorized = errors.New("unauthorized request")
)
