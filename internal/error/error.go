package error

import "errors"

// Pull out shared errors
var (
	ErrUnauthorized     = errors.New("unauthorized request")
	ErrNoSettingCliques = errors.New("records for the network.habitat.clique collection cannot be set directly. this collection is reserved.")
	ErrNoGettingClique  = errors.New("records for the network.habitat.clique cannot be called with GetRecord, use clique XRPC endpoints instead")
)
