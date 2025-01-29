package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/pkg/errors"
)

type controller2 struct {
	ctx            context.Context
	db             hdb.Client
	processManager process.ProcessManager
}

func newController2(ctx context.Context, pm process.ProcessManager, db hdb.Client) (*controller2, error) {
	// Validate types of all input components
	_, ok := pm.(node.Component[process.RestoreInfo])
	if !ok {
		return nil, fmt.Errorf("Process manager of type %T does not implement Component[*node.Process]", pm)
	}

	ctrl := &controller2{
		ctx:            ctx,
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

	err = c.processManager.StartProcess(c.ctx, transition.Process.ID, app.AppInstallation)
	if err != nil {
		// Rollback the state change if the process start failed
		_, err = c.db.ProposeTransitions([]hdb.Transition{
			&node.ProcessStopTransition{
				ProcessID: transition.Process.ID,
			},
		})
		return errors.Wrap(err, "error starting process")
	}
	return nil
}

func (c *controller2) stopProcess(processID node.ProcessID) error {
	procErr := c.processManager.StopProcess(c.ctx, processID)
	// If there was no process found with this ID, continue with the state transition
	// Otherwise this action failed, return an error without the transition
	if procErr != nil && !errors.Is(procErr, process.ErrNoProcFound) {
		// process.ErrNoProcFound is sometimes expected. In this case, still
		// attempt to remove the process from the node state.
		return procErr
	}

	// Only propose transitions if the process exists in state
	_, err := c.db.ProposeTransitions([]hdb.Transition{
		&node.ProcessStopTransition{
			ProcessID: processID,
		},
	})
	return err
}

func (c *controller2) restore(state *node.State) error {
	// Restore processes to the current state
	info := make(map[node.ProcessID]*node.AppInstallation)
	for _, proc := range state.Processes {
		app, ok := state.AppInstallations[proc.AppID]
		if !ok {
			return fmt.Errorf("no app installation found for desired process: ID=%s appID=%s", proc.ID, proc.AppID)
		}
		info[proc.ID] = app.AppInstallation
	}
	return c.processManager.RestoreFromState(c.ctx, info)
}
