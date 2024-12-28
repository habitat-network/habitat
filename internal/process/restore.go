package process

import (
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/controller"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
)

type ProcessRestorer struct {
	processManager ProcessManager
	nodeController controller.NodeController
}

// TODO: Return []error for each failure rather than one
func (r *ProcessRestorer) Restore(restoreEvent hdb.StateUpdate) error {
	nodeState := restoreEvent.NewState().(*node.State)
	for _, process := range nodeState.Processes {
		app, err := nodeState.GetAppByID(process.AppID)
		if err != nil {
			log.Error().Msgf("Error getting app %s: %s", process.AppID, err)
			return err
		}

		switch process.State {
		case node.ProcessStateRunning:
			err = r.processManager.StartProcess(process.Process, app.AppInstallation)
			if err != nil {
				log.Error().Msgf("Error starting process %s: %s", process.ID, err)
				return err
			}
		case node.ProcessStateStarting:
			log.Info().Msgf("Process %s was in starting state, starting process", process.ID)
			err = r.processManager.StartProcess(process.Process, app.AppInstallation)
			if err != nil {
				log.Error().Msgf("Error starting process %s: %s", process.ID, err)
				return err
			}

			err = r.nodeController.SetProcessRunning(process.ID)
			if err != nil {
				log.Error().Msgf("Error setting process %s to running: %s", process.ID, err)
				return err
			}
		}
	}

	return nil
}
