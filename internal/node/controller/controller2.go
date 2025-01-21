package controller

import (
	"encoding/json"
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/pkg/errors"
)

type controller2 struct {
	db             hdb.Client
	processManager process.ProcessManager
}

func newController2(pm process.ProcessManager, db hdb.Client) (*controller2, error) {
	// Validate types of all input components
	_, ok := pm.(node.Component[process.RestoreInfo])
	if !ok {
		return nil, fmt.Errorf("Process manager of type %T does not implement Component[*node.Process]", pm)
	}

	ctrl := &controller2{
		processManager: pm,
		db:             db,
	}

	return ctrl, nil
}

func (c *controller2) getNodeState() (*node.State, error) {
	var nodeState node.State
	err := json.Unmarshal(c.db.Bytes(), &nodeState)
	if err != nil {
		return nil, err
	}
	return &nodeState, nil
}

func (c *controller2) startProcess(installationID string) error {
	state, err := c.getNodeState()
	if err != nil {
		return err
	}
	app, ok := state.AppInstallations[installationID]
	if !ok {
		return fmt.Errorf("app with ID %s not found", installationID)
	}

	transition, err := node.GenProcessStartTransition(installationID, state)
	if err != nil {
		return errors.Wrap(err, "error creating transition")
	}

	_, err = c.db.ProposeTransitions([]hdb.Transition{
		transition,
	})
	if err != nil {
		return errors.Wrap(err, "error proposing transition")
	}

	proc := transition.Process
	err = c.processManager.StartProcess(proc, app.AppInstallation)
	if err != nil {
		return errors.Wrap(err, "error starting process")
	}
	return nil
}

func (c *controller2) stopProcess(processID string) error {
	_, err := c.db.ProposeTransitions([]hdb.Transition{
		&node.ProcessStopTransition{
			ProcessID: processID,
		},
	})
	if err != nil {
		return err
	}

	err = c.processManager.StopProcess(processID)
	if err != nil {
		return errors.Wrap(err, "error stopping process")
	}
	return nil
}

func (c *controller2) restore(state *node.State) error {
	// Restore processes to the current state
	err := c.processManager.RestoreFromState(process.RestoreInfo{Procs: state.Processes, Apps: state.AppInstallations})
	if err != nil {
		return err
	}
	return nil
}
