package package_manager

import (
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
)

type PackageManagerRestorer struct {
	packageManagers map[node.DriverType]PackageManager
}

func (r *PackageManagerRestorer) Restore(restoreEvent hdb.StateUpdate) error {
	nodeState := restoreEvent.NewState().(*node.State)
	for _, app := range nodeState.AppInstallations {
		// Only try to install the app if it was in the state "installing"
		if app.State == node.AppLifecycleStateInstalling {
			appDriver, err := r.getAppDriver(&app.Package)
			if err != nil {
				return err
			}

			err = appDriver.InstallPackage(&app.Package, app.Version)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *PackageManagerRestorer) getAppDriver(spec *node.Package) (PackageManager, error) {
	driver, ok := r.packageManagers[spec.Driver]
	if !ok {
		return nil, fmt.Errorf("driver '%s' not found", spec.Driver)
	}
	return driver, nil
}
