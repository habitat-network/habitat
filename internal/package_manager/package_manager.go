package package_manager

import "github.com/eagraf/habitat-new/core/state/node"

type PackageManager interface {
	Driver() node.DriverType
	IsInstalled(packageSpec *node.Package, version string) (bool, error)
	InstallPackage(packageSpec *node.Package, version string) error
	UninstallPackage(packageSpec *node.Package, version string) error
	// PackageManager is a Component of map[string]*node.AppInstallation
	// Specifically, it should have a RestoreFromState() method on this typ
	node.Component[map[string]*node.AppInstallation]
}
