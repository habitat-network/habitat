package docker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bradenaw/juniper/xslices"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	node_state "github.com/eagraf/habitat-new/internal/node/state"

	"github.com/eagraf/habitat-new/internal/process"
	"github.com/rs/zerolog/log"
)

const (
	habitatLabel = "habitat_proc_id"
)

type dockerDriver struct {
	client *client.Client
}

// dockerDriver implements process.Driver
var _ process.Driver = &dockerDriver{}

func NewDriver(client *client.Client) process.Driver {
	return &dockerDriver{
		client: client,
	}
}

func (d *dockerDriver) Type() node_state.DriverType {
	return node_state.DriverTypeDocker
}

func (d *dockerDriver) StartProcess(ctx context.Context, processID node_state.ProcessID, app *node_state.AppInstallation) error {
	var dockerConfig node_state.AppInstallationConfig
	dockerConfigBytes, err := json.Marshal(app.DriverConfig)
	if err != nil {
		return err
	}

	err = json.Unmarshal(dockerConfigBytes, &dockerConfig)
	if err != nil {
		return err
	}

	exposedPorts := make(nat.PortSet)
	for _, port := range dockerConfig.ExposedPorts {
		exposedPorts[nat.Port(port)] = struct{}{}
	}

	createResp, err := d.client.ContainerCreate(ctx, &container.Config{
		Image:        fmt.Sprintf("%s/%s:%s", app.RegistryURLBase, app.RegistryPackageID, app.RegistryPackageTag),
		ExposedPorts: exposedPorts,
		Env:          dockerConfig.Env,
		Labels: map[string]string{
			habitatLabel: string(processID),
		},
	}, &container.HostConfig{
		PortBindings: dockerConfig.PortBindings,
		Mounts:       dockerConfig.Mounts,
	}, nil, nil, "")
	if err != nil {
		return err
	}

	err = d.client.ContainerStart(ctx, createResp.ID, container.StartOptions{})
	if err != nil {
		return err
	}

	log.Info().Msgf("Started docker container %s", createResp.ID)

	return nil
}

func (d *dockerDriver) getContainerWithProcessID(ctx context.Context, processID node_state.ProcessID) (types.Container, bool, error) {
	labelVal := habitatLabel + "=" + string(processID)
	ctrs, err := d.client.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", labelVal),
		),
	})
	if err != nil {
		return types.Container{}, false, err
	}

	if len(ctrs) > 1 {
		return types.Container{}, false, fmt.Errorf("Found multiple containers with label=%s: %v", labelVal, ctrs)
	} else if len(ctrs) == 0 {
		return types.Container{}, false, nil
	}

	return ctrs[0], true, nil
}

func (d *dockerDriver) StopProcess(ctx context.Context, processID node_state.ProcessID) error {
	ctr, ok, err := d.getContainerWithProcessID(ctx, processID)
	if err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("%w: %s", process.ErrNoProcFound, processID)
	}

	return d.client.ContainerStop(ctx, ctr.ID, container.StopOptions{})
}

func (d *dockerDriver) IsRunning(ctx context.Context, id node_state.ProcessID) (bool, error) {
	_, ok, err := d.getContainerWithProcessID(ctx, id)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (d *dockerDriver) ListRunningProcesses(ctx context.Context) ([]node_state.ProcessID, error) {
	ctrs, err := d.client.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", habitatLabel),
		),
	})
	if err != nil {
		return nil, err
	}
	return xslices.Map(ctrs, func(ctr types.Container) node_state.ProcessID {
		return node_state.ProcessID(ctr.Labels[habitatLabel])
	}), nil
}
