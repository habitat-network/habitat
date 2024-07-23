package package_manager

import (
	"encoding/json"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/controller"
	"github.com/eagraf/habitat-new/internal/node/hdb"
)

type InstallAppExecutor struct {
	packageManager PackageManager
	nodeController controller.NodeController
}

func (e *InstallAppExecutor) TransitionType() string {
	return node.TransitionStartInstallation
}

func (e *InstallAppExecutor) ShouldExecute(update hdb.StateUpdate) (bool, error) {
	var t node.StartInstallationTransition
	err := json.Unmarshal(update.Transition(), &t)
	if err != nil {
		return false, err
	}
	spec := t.Package

	isInstalled, err := e.packageManager.IsInstalled(&spec, t.Version)
	if err != nil {
		return false, err
	}
	if isInstalled {
		return false, nil
	}
	return true, nil
}

func (e *InstallAppExecutor) Execute(update hdb.StateUpdate) error {
	var t node.StartInstallationTransition
	err := json.Unmarshal(update.Transition(), &t)
	if err != nil {
		return err
	}
	err = e.packageManager.InstallPackage(&t.Package, t.Version)
	if err != nil {
		return err
	}

	return nil
}

func (e *InstallAppExecutor) PostHook(update hdb.StateUpdate) error {
	var t node.StartInstallationTransition
	err := json.Unmarshal(update.Transition(), &t)
	if err != nil {
		return err
	}

	// After finishing the installation, update the application's lifecycle state
	appInstallation := t.EnrichedData.AppState
	err = e.nodeController.FinishAppInstallation(t.UserID, appInstallation.ID, appInstallation.RegistryURLBase, appInstallation.RegistryPackageID, t.StartAfterInstallation)
	if err != nil {
		return err
	}

	return nil
}

// FinishInstallExecutor is a state update executor that finishes the installation of an application.
type FinishInstallExecutor struct {
	nodeController controller.NodeController
}

func (e *FinishInstallExecutor) TransitionType() string {
	return node.TransitionFinishInstallation
}

func (e *FinishInstallExecutor) ShouldExecute(update hdb.StateUpdate) (bool, error) {
	return true, nil
}

func (e *FinishInstallExecutor) Execute(update hdb.StateUpdate) error {
	var t node.FinishInstallationTransition
	err := json.Unmarshal(update.Transition(), &t)
	if err != nil {
		return err
	}

	if t.StartAfterInstallation {
		err = e.nodeController.StartProcess(t.AppID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (e *FinishInstallExecutor) PostHook(update hdb.StateUpdate) error {
	// noop
	return nil
}

func NewAppLifecycleSubscriber(packageManager PackageManager, nodeController controller.NodeController) (*hdb.IdempotentStateUpdateSubscriber, error) {
	// TODO this should have a fx cleanup hook to cleanly handle interrupted installs
	// when the node shuts down.
	pmRestorer := &PackageManagerRestorer{
		packageManager: packageManager,
	}

	return hdb.NewIdempotentStateUpdateSubscriber(
		"AppLifecycleSubscriber",
		node.SchemaName,
		[]hdb.IdempotentStateUpdateExecutor{
			&InstallAppExecutor{
				packageManager: packageManager,
				nodeController: nodeController,
			},
			&FinishInstallExecutor{
				nodeController: nodeController,
			},
		},
		pmRestorer,
	)
}
