package web

import (
	"github.com/eagraf/habitat-new/internal/node/state"
	"github.com/eagraf/habitat-new/internal/process"
)

// Currently the implementation is just no-ops because all we need is for the state machine
// to mark the process as started or stopped, in order for files from the web bundle to be
// served.
func NewDriver() process.Driver {
	return process.NewNoopDriver(state.DriverTypeWeb)
}
