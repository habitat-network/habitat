package package_manager

import (
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/core/state/node/test_helpers"
	controller_mocks "github.com/eagraf/habitat-new/internal/node/controller/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestAppInstallSubscriber(t *testing.T) {
	stateUpdate, err := test_helpers.StateUpdateTestHelper(
		&node.StartInstallationTransition{
			UserID: "user1",
			AppInstallation: &node.AppInstallation{
				ID:      "app1",
				Name:    "appname1",
				Version: "v1",
				Package: node.Package{
					Driver:            "test",
					RegistryURLBase:   "registry.com",
					RegistryPackageID: "package1",
				},
			},
			StartAfterInstallation: true,
		},
		&node.State{
			Users: map[string]*node.User{
				"user1": {
					ID: "user1",
				},
			},
		})
	assert.Nil(t, err)

	ctrl := gomock.NewController(t)

	pm := newMockManager()
	nc := controller_mocks.NewMockNodeController(ctrl)

	lifeCycleSubscriber, err := NewAppLifecycleSubscriber(map[string]PackageManager{"test": pm}, nc)
	require.Equal(t, lifeCycleSubscriber.Name(), "AppLifecycleSubscriber")
	require.Nil(t, err)

	installAppExecutor := &InstallAppExecutor{
		packageManagers: map[string]PackageManager{"test": pm},
		nodeController:  nc,
	}

	// Test not installed
	installed, err := pm.IsInstalled(&node.Package{
		Driver:            "test",
		RegistryURLBase:   "registry.com",
		RegistryPackageID: "package1",
	}, "v1")
	require.NoError(t, err)
	require.False(t, installed)

	should, err := installAppExecutor.ShouldExecute(stateUpdate)
	assert.Nil(t, err)
	assert.Equal(t, true, should)

	// Test that ShouldExecute returns false when it is installed
	require.NoError(t, pm.InstallPackage(&node.Package{
		Driver:            "test",
		RegistryURLBase:   "registry.com",
		RegistryPackageID: "package1",
	}, "v1"))

	should, err = installAppExecutor.ShouldExecute(stateUpdate)
	assert.Nil(t, err)
	assert.Equal(t, false, should)

	// Clear pm history to test Execute()
	pm.installed = []*node.Package{}

	err = installAppExecutor.Execute(stateUpdate)
	assert.Nil(t, err)

	nc.EXPECT().FinishAppInstallation(gomock.Eq("user1"), gomock.Any(), gomock.Eq("registry.com"), gomock.Eq("package1"), true).Return(nil).Times(2)

	err = installAppExecutor.PostHook(stateUpdate)
	assert.Nil(t, err)

	// Test installation failure from driver
	require.ErrorIs(t, pm.InstallPackage(&node.Package{
		Driver:            "test",
		RegistryURLBase:   "registry.com",
		RegistryPackageID: "package1",
	}, "v1"), errDuplicate)

	err = installAppExecutor.Execute(stateUpdate)
	assert.NotNil(t, err)

	assert.Equal(t, node.TransitionStartInstallation, installAppExecutor.TransitionType())
	require.NoError(t, lifeCycleSubscriber.ConsumeEvent(stateUpdate))
}

func TestFinishInstallSubscriber(t *testing.T) {
	stateUpdate, err := test_helpers.StateUpdateTestHelper(
		&node.FinishInstallationTransition{
			UserID:                 "user1",
			AppID:                  "app1",
			RegistryURLBase:        "registry.com",
			RegistryAppID:          "package1",
			StartAfterInstallation: true,
		},
		&node.State{
			Users: map[string]*node.User{
				"user1": {
					ID: "user1",
				},
			},
			AppInstallations: map[string]*node.AppInstallationState{
				"app1": {
					AppInstallation: &node.AppInstallation{
						ID:      "app1",
						Name:    "appname1",
						Version: "v1",
						Package: node.Package{
							Driver:            "test",
							RegistryURLBase:   "registry.com",
							RegistryPackageID: "package1",
						},
					},
					State: node.AppLifecycleStateInstalling,
				},
			},
		})
	assert.Nil(t, err)

	ctrl := gomock.NewController(t)

	nc := controller_mocks.NewMockNodeController(ctrl)
	pm := newMockManager()

	lifeCycleSubscriber, err := NewAppLifecycleSubscriber(map[string]PackageManager{"test": pm}, nc)
	require.Equal(t, lifeCycleSubscriber.Name(), "AppLifecycleSubscriber")
	require.Nil(t, err)

	finishAppInstallExecutor := &FinishInstallExecutor{
		nodeController: nc,
	}

	should, err := finishAppInstallExecutor.ShouldExecute(stateUpdate)
	assert.Nil(t, err)
	assert.Equal(t, true, should)

	// Test that executing is a no-op
	nc.EXPECT().StartProcess(gomock.Eq("app1")).Return(nil).Times(1)
	err = finishAppInstallExecutor.Execute(stateUpdate)
	assert.Nil(t, err)

	err = finishAppInstallExecutor.PostHook(stateUpdate)
	assert.Nil(t, err)
}

func TestFinishInstallSubscriberNoAutoStart(t *testing.T) {
	stateUpdate, err := test_helpers.StateUpdateTestHelper(
		&node.FinishInstallationTransition{
			UserID:                 "user1",
			AppID:                  "app1",
			RegistryURLBase:        "registry.com",
			RegistryAppID:          "package1",
			StartAfterInstallation: false,
		},
		&node.State{
			Users: map[string]*node.User{
				"user1": {
					ID: "user1",
				},
			},
			AppInstallations: map[string]*node.AppInstallationState{
				"app1": {
					AppInstallation: &node.AppInstallation{
						ID:      "app1",
						Name:    "appname1",
						Version: "v1",
						Package: node.Package{
							Driver:            "test",
							RegistryURLBase:   "registry.com",
							RegistryPackageID: "package1",
						},
					},
					State: node.AppLifecycleStateInstalling,
				},
			},
		})
	assert.Nil(t, err)

	ctrl := gomock.NewController(t)

	nc := controller_mocks.NewMockNodeController(ctrl)
	pm := newMockManager()

	lifeCycleSubscriber, err := NewAppLifecycleSubscriber(map[string]PackageManager{"test": pm}, nc)
	require.Equal(t, lifeCycleSubscriber.Name(), "AppLifecycleSubscriber")
	require.Nil(t, err)

	finishAppInstallExecutor := &FinishInstallExecutor{
		nodeController: nc,
	}

	should, err := finishAppInstallExecutor.ShouldExecute(stateUpdate)
	assert.Nil(t, err)
	assert.Equal(t, true, should)

	nc.EXPECT().StartProcess(gomock.Eq("app1")).Return(nil).Times(0)
	err = finishAppInstallExecutor.Execute(stateUpdate)
	assert.Nil(t, err)

	// Test that we start the process.

	err = finishAppInstallExecutor.PostHook(stateUpdate)
	assert.Nil(t, err)
}
