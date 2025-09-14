package process

import (
	"context"
	"errors"

	"github.com/eagraf/habitat-new/internal/app"
)

var (
	ErrNoProcFound           = errors.New("no process found for given id")
	ErrProcessAlreadyRunning = errors.New("process already running")
)

// Driver is the construct which manages processes for a given type.
// For example, web / docker processes will have different contracts to start and stop processes
// but these can all be implemented behind the Driver interface for easy interaction at the node controller level.
type Driver interface {
	Type() app.DriverType
	// Start a process for the given AppInstallation tagged with the given ID
	StartProcess(context.Context, ID, *app.Installation) error
	// Stop the process according to the given ID
	StopProcess(context.Context, ID) error
	// Returns whether the given process is currently running and a non-nil error if that cannot be determined
	IsRunning(context.Context, ID) (bool, error)
	// Returns all running process or a non-nil error if that information cannot be extracted
	ListRunningProcesses(context.Context) ([]ID, error)
}

type noopDriver struct {
	driverType app.DriverType
}

func NewNoopDriver(driverType app.DriverType) Driver {
	return &noopDriver{driverType: driverType}
}

func (d *noopDriver) Type() app.DriverType {
	return app.DriverTypeNoop
}

func (d *noopDriver) StartProcess(context.Context, ID, *app.Installation) error {
	return nil
}

func (d *noopDriver) StopProcess(context.Context, ID) error {
	return nil
}

func (d *noopDriver) IsRunning(context.Context, ID) (bool, error) {
	// No-op driver doesn't do anything so any given process ID is "never running" from its perspective
	return false, nil
}

func (d *noopDriver) ListRunningProcesses(context.Context) ([]ID, error) {
	// No-op driver doesn't do anything so there are no "running" processes from its perspective
	return []ID{}, nil
}
