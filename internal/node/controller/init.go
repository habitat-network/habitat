package controller

import (
	"path/filepath"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
	types "github.com/eagraf/habitat-new/core/api"
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/google/uuid"
)

func generatePDSAppConfig(nodeConfig *config.NodeConfig) types.PostAppRequest {

	pdsMountDir := filepath.Join(nodeConfig.HabitatPath(), "pds")

	// TODO @eagraf - unhardcode as much of this as possible
	return types.PostAppRequest{
		AppInstallation: &node.AppInstallation{
			Name:    "pds",
			Version: "1",
			UserID:  constants.RootUserID,
			Package: node.Package{
				Driver: "docker",
				DriverConfig: map[string]interface{}{
					"env": []string{
						"PDS_HOSTNAME=ethangraf.com",
						"PDS_DATA_DIRECTORY=/pds",
						"PDS_BLOBSTORE_DISK_LOCATION=/pds/blocks",
						"PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX=5290bb1866a03fb23b09a6ffd64d21f6a4ebf624eaa301930eeb81740699239c",
						"PDS_JWT_SECRET=bd6df801372d7058e1ce472305d7fc2e",
						"PDS_ADMIN_PASSWORD=password",
					},
					"mounts": []mount.Mount{
						{
							Type:   "bind",
							Source: pdsMountDir,
							Target: "/pds",
						},
					},
					"exposed_ports": []string{"5000"},
					"port_bindings": map[nat.Port][]nat.PortBinding{
						"3000/tcp": {
							{
								HostIP:   "127.0.0.1",
								HostPort: "5000",
							},
						},
					},
				},
				RegistryURLBase:    "registry.hub.docker.com",
				RegistryPackageID:  "ethangraf/pds",
				RegistryPackageTag: "latest",
			},
		},
		ReverseProxyRules: []*node.ReverseProxyRule{
			{
				Type:    "redirect",
				Matcher: "/xrpc",
				Target:  "http://host.docker.internal:5000/xrpc",
			},
		},
	}
}

// TODO this is basically a placeholder until we actually have a way of generating
// the certificate for the node.
func generateInitState(nodeConfig *config.NodeConfig) (*node.NodeState, error) {
	nodeUUID := uuid.New().String()

	rootCert := nodeConfig.RootUserCertB64()

	initState, err := node.GetEmptyStateForVersion(node.LatestVersion)
	if err != nil {
		return nil, err
	}

	initState.NodeID = nodeUUID
	initState.Users[constants.RootUserID] = &node.User{
		ID:          constants.RootUserID,
		Username:    constants.RootUsername,
		Certificate: rootCert,
	}

	return initState, nil
}

func initTranstitions(nodeConfig *config.NodeConfig) ([]hdb.Transition, error) {

	initState, err := generateInitState(nodeConfig)
	if err != nil {
		return nil, err
	}

	transitions := []hdb.Transition{
		&node.InitalizationTransition{
			InitState: initState,
		},
	}

	pdsAppConfig := generatePDSAppConfig(nodeConfig)
	defaultApplications := []types.PostAppRequest{
		pdsAppConfig,
	}

	for _, app := range defaultApplications {
		transitions = append(transitions, &node.StartInstallationTransition{
			UserID:          constants.RootUserID,
			AppInstallation: app.AppInstallation,
			NewProxyRules:   app.ReverseProxyRules,
		})
	}
	return transitions, nil
}
