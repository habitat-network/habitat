package process

import "github.com/eagraf/habitat-new/core/state/node"

type Driver interface {
	Type() string
	StartProcess(*node.Process, *node.AppInstallation) (string, error)
	StopProcess(extProcessID string) error
}

type noopDriver struct {
	driverType string
}

func NewNoopDriver(driverType string) Driver {
	return &noopDriver{driverType: driverType}
}

func (d *noopDriver) Type() string {
	return d.driverType
}

func (d *noopDriver) StartProcess(*node.Process, *node.AppInstallation) (string, error) {
	return "", nil
}

func (d *noopDriver) StopProcess(extProcessID string) error {
	return nil
}
