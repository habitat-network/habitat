package process

import (
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
)

type runningProcess struct {
	*node.Process
	driverID string
}

type RestoreInfo struct {
	Procs map[string]*node.Process
	Apps  map[string]*node.AppInstallationState
}

type ProcessManager interface {
	ListProcesses() ([]*node.Process, error)
	StartProcess(*node.Process, *node.AppInstallation) error
	StopProcess(extProcessID string) error
	// Returns process state, true if exists, otherwise nil, false to indicate non-existence
	GetProcess(processID string) (*node.Process, bool)
	// ProcessManager should implement Component -- specifically, restore state given a set of processes and apps
	node.Component[RestoreInfo]
}

type baseProcessManager struct {
	processDrivers map[string]Driver
	processes      map[string]*runningProcess
}

func NewProcessManager(drivers []Driver) ProcessManager {
	pm := &baseProcessManager{
		processDrivers: make(map[string]Driver),
		processes:      make(map[string]*runningProcess),
	}
	for _, driver := range drivers {
		pm.processDrivers[driver.Type()] = driver
	}
	return pm
}

func (pm *baseProcessManager) ListProcesses() ([]*node.Process, error) {
	processList := make([]*node.Process, 0, len(pm.processes))
	for _, process := range pm.processes {
		processList = append(processList, process.Process)
	}

	return processList, nil
}

func (pm *baseProcessManager) GetProcess(processID string) (*node.Process, bool) {
	proc, ok := pm.processes[processID]
	if !ok {
		return nil, false
	}
	return proc.Process, true
}

func (pm *baseProcessManager) StartProcess(process *node.Process, app *node.AppInstallation) error {
	proc, ok := pm.processes[process.ID]
	if ok {
		return fmt.Errorf("error starting process: process %s already found: %v", process.ID, proc)
	}

	driver, ok := pm.processDrivers[app.Driver]
	if !ok {
		return fmt.Errorf("error starting process: driver %s not found", app.Driver)
	}

	extProcessID, err := driver.StartProcess(process, app)
	if err != nil {
		return err
	}

	pm.processes[process.ID] = &runningProcess{
		Process:  process,
		driverID: extProcessID,
	}
	return nil
}

func (pm *baseProcessManager) StopProcess(processID string) error {
	process, ok := pm.processes[processID]
	if !ok {
		return fmt.Errorf("error stopping process: process %s not found", processID)
	}

	driver, ok := pm.processDrivers[process.Driver]
	if !ok {
		return fmt.Errorf("error stopping process: driver %s not found", process.Driver)
	}

	err := driver.StopProcess(process.driverID)
	if err != nil {
		return err
	}

	delete(pm.processes, processID)

	return nil
}

func (pm *baseProcessManager) SupportedTransitionTypes() []hdb.TransitionType {
	return []hdb.TransitionType{
		hdb.TransitionStartProcess,
		hdb.TransitionStopProcess,
	}
}

func (pm *baseProcessManager) RestoreFromState(state RestoreInfo) error {
	for _, process := range state.Procs {
		app, ok := state.Apps[process.AppID]
		if !ok {
			return fmt.Errorf("No app installation found for given AppID %s", process.AppID)
		}

		err := pm.StartProcess(process, app.AppInstallation)
		if err != nil {
			return fmt.Errorf("Error starting process %s: %s", process.ID, err)
		}
	}
	return nil
}
