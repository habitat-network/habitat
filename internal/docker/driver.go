package docker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/rs/zerolog/log"
)

const (
	habitatLabel   = "habitat_proc_id"
	errNoProcFound = "no container found with label %s"
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

func (d *dockerDriver) Type() string {
	return constants.AppDriverDocker
}

// StartProcess helps implement dockerDriver
func (d *dockerDriver) StartProcess(ctx context.Context, process *node.Process, app *node.AppInstallation) error {
	var dockerConfig node.AppInstallationConfig
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
			habitatLabel: string(process.ID),
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

func (d *dockerDriver) StopProcess(ctx context.Context, processID node.ProcessID) error {
	ctrs, err := d.client.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", habitatLabel+"="+string(processID)),
		),
	})
	if err != nil {
		return err
	}

	if len(ctrs) > 1 {
		return fmt.Errorf("Got multiple processes with label %s: %v", habitatLabel, ctrs)
	} else if len(ctrs) == 0 {
		return fmt.Errorf(errNoProcFound, habitatLabel)
	}

	err = d.client.ContainerStop(ctx, ctrs[0].ID, container.StopOptions{})
	if err != nil {
		return err
	}

	return nil
}
