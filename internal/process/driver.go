package process

import (
	"context"

	"github.com/eagraf/habitat-new/core/state/node"
)

type Driver interface {
	Type() string
	StartProcess(context.Context, *node.Process, *node.AppInstallation) error
	StopProcess(context.Context, node.ProcessID) error
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

func (d *noopDriver) StartProcess(context.Context, *node.Process, *node.AppInstallation) error {
	return nil
}

func (d *noopDriver) StopProcess(context.Context, node.ProcessID) error {
	return nil
}
