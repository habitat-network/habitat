package app

import "github.com/eagraf/habitat-new/internal/types"

type PackageManager interface {
	Driver() DriverType
	IsInstalled(packageSpec *Package, version string) (bool, error)
	InstallPackage(packageSpec *Package, version string) error
	UninstallPackage(packageSpec *Package, version string) error
	// PackageManager is a Component of map[string]*node.AppInstallation
	// Specifically, it should have a RestoreFromState() method on this typ
	types.Component[map[string]*Installation]
}
