package process

import (
	"fmt"
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/core/state/node/test_helpers"
	controller_mocks "github.com/eagraf/habitat-new/internal/node/controller/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestProcessRestorer(t *testing.T) {

	mockDriver := newMockDriver()
	pm := NewProcessManager([]Driver{mockDriver})

	ctrl := gomock.NewController(t)
	nc := controller_mocks.NewMockNodeController(ctrl)

	pr := &ProcessRestorer{
		processManager: pm,
		nodeController: nc,
	}

	state := &node.State{
		Users: map[string]*node.User{
			"user1": {
				ID: "user1",
			},
		},
		AppInstallations: map[string]*node.AppInstallationState{
			"app1": {
				AppInstallation: &node.AppInstallation{
					ID:   "app1",
					Name: "appname1",
					Package: node.Package{
						Driver: "test",
					},
				},
			},
			"app2": {
				AppInstallation: &node.AppInstallation{
					ID:   "app2",
					Name: "appname2",
					Package: node.Package{
						Driver: "test",
					},
				},
			},
			"app3": {
				AppInstallation: &node.AppInstallation{
					ID:   "app3",
					Name: "appname3",
					Package: node.Package{
						Driver: "test",
					},
				},
			},

			"app4": {
				AppInstallation: &node.AppInstallation{
					ID:   "app4",
					Name: "appname4",
					Package: node.Package{
						Driver: "test",
					},
				},
			},
		},
		Processes: map[string]*node.ProcessState{
			"proc1": {
				Process: &node.Process{
					ID:     "proc1",
					AppID:  "app1",
					Driver: "test",
				},
				State: node.ProcessStateRunning,
			},
			// This process was not in a running state, but should be started
			"proc2": {
				Process: &node.Process{
					ID:     "proc2",
					AppID:  "app2",
					Driver: "test",
				},
				State: node.ProcessStateStarting,
			},
			// Error out when restoring starting
			"proc3": {
				Process: &node.Process{
					ID:     "proc3",
					AppID:  "app3",
					Driver: "test",
				},
				State: node.ProcessStateStarting,
			},
			// Error out when restoring running
			"proc4": {
				Process: &node.Process{
					ID:    "proc4",
					AppID: "app4",
				},
				State: node.ProcessStateRunning,
			},
		},
	}
	restoreUpdate, err := test_helpers.StateUpdateTestHelper(&node.InitalizationTransition{}, state)
	require.Nil(t, err)

	nc.EXPECT().SetProcessRunning("proc2").Times(1)
	nc.EXPECT().SetProcessRunning("proc3").Times(1)

	err = pr.Restore(restoreUpdate)
	require.Nil(t, err)

	require.Len(t, mockDriver.log, 4)
	for _, entry := range mockDriver.log {
		require.True(t, entry.isStart)
	}

	// Test ListProcesses() and StopProcess()
	procs, err := pm.ListProcesses()
	require.NoError(t, err)
	require.Len(t, procs, 4)

	require.NoError(t, pm.StopProcess("proc2"))
	require.ErrorContains(t, pm.StopProcess("proc4"), "driver  not found")

	procs, err = pm.ListProcesses()
	require.NoError(t, err)
	require.Len(t, procs, 3)

	mockDriver.returnErr = fmt.Errorf("test error")
	err = pm.StartProcess(&node.Process{
		ID:    "proc5",
		AppID: "app5",
	}, &node.AppInstallation{
		ID:   "app5",
		Name: "appname5",
		Package: node.Package{
			Driver: "test",
		},
	})
	require.ErrorContains(t, err, "test error")

	restoreUpdate, err = test_helpers.StateUpdateTestHelper(&node.InitalizationTransition{}, state)
	require.NoError(t, err)
	err = pr.Restore(restoreUpdate)
	require.ErrorContains(t, err, "test error")
}
