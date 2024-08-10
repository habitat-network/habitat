package docker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/package_manager"
	"github.com/eagraf/habitat-new/internal/node/processes"
	"github.com/rs/zerolog/log"
)

// Example docker driver configuration for a container configuration:

/**
	"driver_config": {
		"env": [
			"PDS_HOSTNAME=ethangraf.com",
			"PDS_DATA_DIRECTORY=/pds",
		],
		"mounts": [
			{
				"Type": "bind",
				"Source": "/Users/ethan/code/habitat/habitat-new/.habitat/pds",
				"Target": "/pds"
			}
		],
		"exposed_ports": [ "5000" ],
		"port_bindings": {
			"3000/tcp": [
				{
					"hostIp": "127.0.0.1",
					"hostPort": "5000"
				}
			]
		}
	}
**/

// AppInstallationConfig is a struct to hold the configuration for a docker container
// Most of these types are taken directly from the Docker Go SDK
type AppInstallationConfig struct {
	// ExposedPorts is a slice of ports exposed by the docker container
	ExposedPorts []string `json:"exposed_ports"`
	// Env is a slice of environment variables to be set in the container, specified as KEY=VALUE
	Env []string `json:"env"`
	// PortBindings is a map of ports to bind on the host to ports in the container. Host IPs can be specified as well
	PortBindings nat.PortMap `json:"port_bindings"`
	// Mounts is a slice of mounts to be mounted in the container
	Mounts []mount.Mount `json:"mounts"`
}

type AppDriver struct {
	client *client.Client
}

func (d *AppDriver) Driver() string {
	return constants.AppDriverDocker
}

func (d *AppDriver) IsInstalled(packageSpec *node.Package, version string) (bool, error) {
	// TODO review all contexts we create.
	repoURL := repoURLFromPackage(packageSpec, version)
	images, err := d.client.ImageList(context.Background(), types.ImageListOptions{
		Filters: filters.NewArgs(
			filters.Arg("reference", repoURL),
		),
	})
	if err != nil {
		return false, err
	}
	return len(images) > 0, nil
}

// Implement the package manager interface
func (d *AppDriver) InstallPackage(packageSpec *node.Package, version string) error {
	if packageSpec.Driver != constants.AppDriverDocker {
		return fmt.Errorf("invalid package driver: %s, expected docker", packageSpec.Driver)
	}

	repoURL := repoURLFromPackage(packageSpec, version)
	_, err := d.client.ImagePull(context.Background(), repoURL, types.ImagePullOptions{})
	if err != nil {
		return err
	}

	log.Info().Msgf("Pulled image %s", repoURL)
	return nil
}

func (d *AppDriver) UninstallPackage(packageURL *node.Package, version string) error {
	repoURL := repoURLFromPackage(packageURL, version)
	_, err := d.client.ImageRemove(context.Background(), repoURL, types.ImageRemoveOptions{})
	if err != nil {
		return err
	}
	return nil
}

type ProcessDriver struct {
	client *client.Client
}

func (d *ProcessDriver) Type() string {
	return constants.AppDriverDocker
}

// StartProcess helps implement processes.ProcessDriver
func (d *ProcessDriver) StartProcess(process *node.Process, app *node.AppInstallation) (string, error) {

	var dockerConfig AppInstallationConfig
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

func (d *ProcessDriver) StopProcess(extProcessID string) error {
	err := d.client.ContainerStop(context.Background(), extProcessID, container.StopOptions{})
	if err != nil {
		return err
	}

	return nil
}

type Driver struct {
	PackageManager package_manager.PackageManager
	ProcessDriver  processes.ProcessDriver `group:"process_drivers"`
}

func NewDriver() (Driver, error) {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create docker client")
	}

	res := Driver{
		PackageManager: &AppDriver{
			client: dockerClient,
		},
		ProcessDriver: &ProcessDriver{
			client: dockerClient,
		},
	}

	return res, nil
}
