package package_manager

import (
	"github.com/eagraf/habitat-new/internal/node/state"
)

type PackageManager interface {
	Driver() state.DriverType
	IsInstalled(packageSpec *state.Package, version string) (bool, error)
	InstallPackage(packageSpec *state.Package, version string) error
	UninstallPackage(packageSpec *state.Package, version string) error
	// PackageManager is a Component of map[string]*node.AppInstallation
	// Specifically, it should have a RestoreFromState() method on this typ
	state.Component[map[string]*state.AppInstallation]
}
