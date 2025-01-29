package package_manager

import (
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/core/state/node/test_helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestore(t *testing.T) {
	restoreUpdate, err := test_helpers.StateUpdateTestHelper(&node.InitalizationTransition{}, &node.State{
		Users: map[string]*node.User{
			"user1": {
				ID: "user1",
			},
		},
		AppInstallations: map[string]*node.AppInstallationState{
			"app1": {
				AppInstallation: &node.AppInstallation{
					ID: "app1",
					Package: node.Package{
						Driver: node.DriverTypeNoop,
					},
				},
				State: node.AppLifecycleStateInstalling,
			},
			"app2": {
				AppInstallation: &node.AppInstallation{
					ID: "app2",
					Package: node.Package{
						Driver: node.DriverTypeNoop,
					},
				},
				State: node.AppLifecycleStateInstalled,
			},
		},
	})
	assert.Nil(t, err)

	pm := newMockManager()

	pmRestorer := &PackageManagerRestorer{
		packageManagers: map[node.DriverType]PackageManager{
			node.DriverTypeNoop: pm,
		},
	}

	err = pmRestorer.Restore(restoreUpdate)
	assert.Nil(t, err)
	require.Len(t, pm.installed, 1)

}
