package error

import "errors"

// Pull out shared errors
var (
	ErrUnauthorized     = errors.New("unauthorized request")
	ErrNoSettingCliques = errors.New("records for the network.habitat.clique collection cannot be set directly as this collection is reserved")
)
