package web

import (
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/constants"
)

// Currently the implementation is just no-ops because all we need is for the state machine
// to mark the process as started or stopped, in order for files from the web bundle to be
// served.
type ProcessDriver struct {
}

func NewProcessDriver() *ProcessDriver {
	return &ProcessDriver{}
}

func (d *ProcessDriver) Type() string {
	return constants.AppDriverWeb
}

func (d *ProcessDriver) StartProcess(process *node.Process, app *node.AppInstallation) (string, error) {
	// noop
	return "", nil
}

func (d *ProcessDriver) StopProcess(extProcessID string) error {
	// noop
	return nil
}
