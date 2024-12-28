package package_manager

import (
	"encoding/json"
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/controller"
	"github.com/eagraf/habitat-new/internal/node/hdb"
)

type InstallAppExecutor struct {
	packageManagers map[string]PackageManager
	nodeController  controller.NodeController
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

	appDriver, err := e.getAppDriver(&spec)
	if err != nil {
		return false, err
	}

	// Use the driver to check if the package is already installed.
	isInstalled, err := appDriver.IsInstalled(&spec, t.Version)
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

	appDriver, err := e.getAppDriver(&t.Package)
	if err != nil {
		return err
	}

	err = appDriver.InstallPackage(&t.Package, t.Version)
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

func (e *InstallAppExecutor) getAppDriver(pkg *node.Package) (PackageManager, error) {
	// Get the right driver for this type of package.
	pkgDriver, ok := e.packageManagers[pkg.Driver]
	if !ok {
		return nil, fmt.Errorf("no package manager found for driver '%s'", pkg.Driver)
	}
	return pkgDriver, nil
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

func NewAppLifecycleSubscriber(packageManagers map[string]PackageManager, nodeController controller.NodeController) (*hdb.IdempotentStateUpdateSubscriber, error) {
	// TODO this should have a fx cleanup hook to cleanly handle interrupted installs
	// when the node shuts down.
	pmRestorer := &PackageManagerRestorer{
		packageManagers: packageManagers,
	}

	return hdb.NewIdempotentStateUpdateSubscriber(
		"AppLifecycleSubscriber",
		node.SchemaName,
		[]hdb.IdempotentStateUpdateExecutor{
			&InstallAppExecutor{
				packageManagers: packageManagers,
				nodeController:  nodeController,
			},
			&FinishInstallExecutor{
				nodeController: nodeController,
			},
		},
		pmRestorer,
	)
}
