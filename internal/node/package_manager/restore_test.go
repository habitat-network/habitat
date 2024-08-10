package package_manager

import (
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/core/state/node/test_helpers"
	"github.com/eagraf/habitat-new/internal/node/package_manager/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
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
						Driver: "test",
					},
				},
				State: node.AppLifecycleStateInstalling,
			},
			"app2": {
				AppInstallation: &node.AppInstallation{
					ID: "app2",
					Package: node.Package{
						Driver: "test",
					},
				},
				State: node.AppLifecycleStateInstalled,
			},
		},
	})
	assert.Nil(t, err)

	ctrl := gomock.NewController(t)

	pm := mocks.NewMockPackageManager(ctrl)

	pmRestorer := &PackageManagerRestorer{
		packageManagers: map[string]PackageManager{
			"test": pm,
		},
	}

	pm.EXPECT().InstallPackage(gomock.Any(), gomock.Any()).Times(1)

	err = pmRestorer.Restore(restoreUpdate)
	assert.Nil(t, err)

}
