package process

import (
	"context"
	"errors"

	"github.com/eagraf/habitat-new/internal/node/state"
)

var (
	ErrNoProcFound           = errors.New("no process found for given id")
	ErrProcessAlreadyRunning = errors.New("process already running")
)

// Driver is the construct which manages processes for a given type.
// For example, web / docker processes will have different contracts to start and stop processes
// but these can all be implemented behind the Driver interface for easy interaction at the node controller level.
type Driver interface {
	Type() state.DriverType
	// Start a process for the given AppInstallation tagged with the given ID
	StartProcess(context.Context, state.ProcessID, *state.AppInstallation) error
	// Stop the process according to the given ID
	StopProcess(context.Context, state.ProcessID) error
	// Returns whether the given process is currently running and a non-nil error if that cannot be determined
	IsRunning(context.Context, state.ProcessID) (bool, error)
	// Returns all running process or a non-nil error if that information cannot be extracted
	ListRunningProcesses(context.Context) ([]state.ProcessID, error)
}

type noopDriver struct {
	driverType state.DriverType
}

func NewNoopDriver(driverType state.DriverType) Driver {
	return &noopDriver{driverType: driverType}
}

func (d *noopDriver) Type() state.DriverType {
	return state.DriverTypeNoop
}

func (d *noopDriver) StartProcess(context.Context, state.ProcessID, *state.AppInstallation) error {
	return nil
}

func (d *noopDriver) StopProcess(context.Context, state.ProcessID) error {
	return nil
}

func (d *noopDriver) IsRunning(context.Context, state.ProcessID) (bool, error) {
	// No-op driver doesn't do anything so any given process ID is "never running" from its perspective
	return false, nil
}

func (d *noopDriver) ListRunningProcesses(context.Context) ([]state.ProcessID, error) {
	// No-op driver doesn't do anything so there are no "running" processes from its perspective
	return []state.ProcessID{}, nil
}
