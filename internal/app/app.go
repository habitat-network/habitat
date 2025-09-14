package app

import (
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
)

// Types for managing app installations, mostly related to internal/package_manager
type LifecycleStateType string

const (
	LifecycleStateInstalling LifecycleStateType = "installing"
	LifecycleStateInstalled  LifecycleStateType = "installed"
)

// TODO some fields should be ignored by the REST api
type Installation struct {
	ID       string             `json:"id" yaml:"id"`
	UserID   string             `json:"user_id" yaml:"user_id"`
	Name     string             `json:"name" yaml:"name"`
	Version  string             `json:"version" yaml:"version"`
	State    LifecycleStateType `json:"state"`
	*Package `yaml:",inline"`
}

// InstallationConfig is a struct to hold the configuration for a docker container
// Most of these types are taken directly from the Docker Go SDK
type InstallationConfig struct {
	// ExposedPorts is a slice of ports exposed by the docker container
	ExposedPorts []string `json:"exposed_ports"`
	// Env is a slice of environment variables to be set in the container, specified as KEY=VALUE
	Env []string `json:"env"`
	// PortBindings is a map of ports to bind on the host to ports in the container. Host IPs can be specified as well
	PortBindings nat.PortMap `json:"port_bindings"`
	// Mounts is a slice of mounts to be mounted in the container
	Mounts []mount.Mount `json:"mounts"`
}
