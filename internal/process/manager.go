package process

import (
	"context"
	"errors"
	"fmt"

	"github.com/eagraf/habitat-new/internal/node/state"
)

// Given RestoreInfo, the ProcessManager will attempt to recreate that state.
// Specifically, it will run the given apps tagged with the according processID
type RestoreInfo map[state.ProcessID]*state.AppInstallation

// ProcessManager is a way to manage processes across many different drivers / runtimes
// Right now, all it does is hold a set of drivers and pass through to calls to the Driver interface for each of them
// For that reason, we could consider removing it in the future and simply holding a map[state.DriverType]Driver in the caller to this
type ProcessManager interface {
	// ListAllProcesses returns a list of all running process IDs, across all drivers
	ListRunningProcesses(context.Context) ([]state.ProcessID, error)
	// StartProcess starts a process for the given app installation with the given process ID
	// It is expected that the driver can be derived from AppInstallation
	StartProcess(context.Context, state.ProcessID, *state.AppInstallation) error
	// StopProcess stops the process corresponding to the given process ID
	StopProcess(context.Context, state.ProcessID) error
	// Returns process state, true if exists, otherwise nil, false to indicate non-existence
	IsRunning(context.Context, state.ProcessID) (bool, error)
	// ProcessManager should implement Component -- specifically, restore state given by RestoreInfo
	state.Component[RestoreInfo]
}

var (
	ErrDriverNotFound = errors.New("no driver found")
)

type baseProcessManager struct {
	drivers map[state.DriverType]Driver
}

func NewProcessManager(drivers []Driver) ProcessManager {
	pm := &baseProcessManager{
		drivers: make(map[state.DriverType]Driver),
	}
	for _, driver := range drivers {
		pm.drivers[driver.Type()] = driver
	}
	return pm
}

func (pm *baseProcessManager) ListRunningProcesses(ctx context.Context) ([]state.ProcessID, error) {
	var allProcs []state.ProcessID
	for _, driver := range pm.drivers {
		procs, err := driver.ListRunningProcesses(ctx)
		if err != nil {
			return nil, err
		}
		allProcs = append(allProcs, procs...)
	}
	return allProcs, nil
}

func (pm *baseProcessManager) IsRunning(ctx context.Context, id state.ProcessID) (bool, error) {
	driverType, err := state.DriverFromProcessID(id)
	if err != nil {
		return false, fmt.Errorf("unable to extract driver from process ID %s: %w", id, err)
	}
	driver, ok := pm.drivers[driverType]
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrDriverNotFound, driverType)
	}
	return driver.IsRunning(ctx, id)
}

func (pm *baseProcessManager) StartProcess(ctx context.Context, id state.ProcessID, app *state.AppInstallation) error {
	driver, ok := pm.drivers[app.Driver]
	if !ok {
		return fmt.Errorf("%w: %s", ErrDriverNotFound, app.Driver)
	}
	ok, err := driver.IsRunning(ctx, id)
	if ok && err == nil {
		return fmt.Errorf("%w: %s", ErrProcessAlreadyRunning, id)
	} else if err != nil {
		return err
	}
	return driver.StartProcess(ctx, id, app)
}

func (pm *baseProcessManager) StopProcess(ctx context.Context, id state.ProcessID) error {
	driverType, err := state.DriverFromProcessID(id)
	if err != nil {
		return fmt.Errorf("unable to extract driver from process ID %s: %w", id, err)
	}
	driver, ok := pm.drivers[driverType]
	if !ok {
		return fmt.Errorf("%w: %s", ErrDriverNotFound, driverType)
	}
	return driver.StopProcess(ctx, id)
}

func (pm *baseProcessManager) SupportedTransitionTypes() []state.TransitionType {
	return []state.TransitionType{
		state.TransitionStartProcess,
		state.TransitionStopProcess,
	}
}

func (pm *baseProcessManager) RestoreFromState(ctx context.Context, state RestoreInfo) error {
	for id, app := range state {
		err := pm.StartProcess(ctx, id, app)
		if err != nil {
			return fmt.Errorf("Error starting process %s: %s", id, err)
		}
	}
	return nil
}
