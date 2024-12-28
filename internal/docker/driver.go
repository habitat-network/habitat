package docker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/rs/zerolog/log"
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
func (d *dockerDriver) StartProcess(process *node.Process, app *node.AppInstallation) (string, error) {
	var dockerConfig node.AppInstallationConfig
	dockerConfigBytes, err := json.Marshal(app.DriverConfig)
	if err != nil {
		return "", err
	}

	err = json.Unmarshal(dockerConfigBytes, &dockerConfig)
	if err != nil {
		return "", err
	}

	exposedPorts := make(nat.PortSet)
	for _, port := range dockerConfig.ExposedPorts {
		exposedPorts[nat.Port(port)] = struct{}{}
	}

	createResp, err := d.client.ContainerCreate(context.Background(), &container.Config{
		Image:        fmt.Sprintf("%s/%s:%s", app.RegistryURLBase, app.RegistryPackageID, app.RegistryPackageTag),
		ExposedPorts: exposedPorts,
		Env:          dockerConfig.Env,
	}, &container.HostConfig{
		PortBindings: dockerConfig.PortBindings,
		Mounts:       dockerConfig.Mounts,
	}, nil, nil, "")
	if err != nil {
		return "", err
	}

	err = d.client.ContainerStart(context.Background(), createResp.ID, container.StartOptions{})
	if err != nil {
		return "", err
	}

	log.Info().Msgf("Started container %s", createResp.ID)

	return createResp.ID, nil
}

func (d *dockerDriver) StopProcess(extProcessID string) error {
	err := d.client.ContainerStop(context.Background(), extProcessID, container.StopOptions{})
	if err != nil {
		return err
	}

	return nil
}
