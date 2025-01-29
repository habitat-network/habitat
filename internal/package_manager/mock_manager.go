package package_manager

import (
	"errors"
	"slices"

	"github.com/eagraf/habitat-new/core/state/node"
)

type mockManager struct {
	installed []*node.Package
}

func newMockManager() *mockManager {
	return &mockManager{}
}

var _ PackageManager = &mockManager{}

func (m *mockManager) Driver() node.DriverType {
	return node.DriverTypeUnknown
}

func packageEq(a *node.Package, b *node.Package) bool {
	return a.Driver == b.Driver && a.RegistryURLBase == b.RegistryURLBase && a.RegistryPackageID == b.RegistryPackageID
}

func (m *mockManager) IsInstalled(packageSpec *node.Package, version string) (bool, error) {
	return slices.ContainsFunc(m.installed, func(e *node.Package) bool {
		return packageEq(e, packageSpec)
	}), nil
}

var (
	errDuplicate = errors.New("duplicate install")
)

func (m *mockManager) InstallPackage(packageSpec *node.Package, version string) error {
	if slices.ContainsFunc(m.installed, func(e *node.Package) bool {
		return packageEq(e, packageSpec)
	}) {
		return errDuplicate
	}
	m.installed = append(m.installed, packageSpec)
	return nil
}

func (m *mockManager) UninstallPackage(packageSpec *node.Package, version string) error {
	return nil
}
