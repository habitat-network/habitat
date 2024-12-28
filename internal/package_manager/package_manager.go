package package_manager

import "github.com/eagraf/habitat-new/core/state/node"

type PackageManager interface {
	Driver() string
	IsInstalled(packageSpec *node.Package, version string) (bool, error)
	InstallPackage(packageSpec *node.Package, version string) error
	UninstallPackage(packageSpec *node.Package, version string) error
}
