package processes

import (
	"errors"
	"testing"
	"time"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/core/state/node/test_helpers"
	controller_mocks "github.com/eagraf/habitat-new/internal/node/controller/mocks"
	"github.com/eagraf/habitat-new/internal/node/processes/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestProcessRestorer(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockDriver := mocks.NewMockProcessDriver(ctrl)

	mockDriver.EXPECT().Type().Return("test")
	pm := NewProcessManager([]ProcessDriver{mockDriver})

	nc := controller_mocks.NewMockNodeController(ctrl)

	pr := &ProcessRestorer{
		processManager: pm,
		nodeController: nc,
	}

	restoreUpdate, err := test_helpers.StateUpdateTestHelper(&node.InitalizationTransition{}, &node.State{
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
					ID:    "proc1",
					AppID: "app1",
				},
				State: node.ProcessStateRunning,
			},
			// This process was not in a running state, but should be started
			"proc2": {
				Process: &node.Process{
					ID:    "proc2",
					AppID: "app2",
				},
				State: node.ProcessStateStarting,
			},
			// Error out when restoring starting
			"proc3": {
				Process: &node.Process{
					ID:    "proc3",
					AppID: "app3",
				},
				State: node.ProcessStateStarting,
			},
			// Error out when restoring runnign
			"proc4": {
				Process: &node.Process{
					ID:    "proc4",
					AppID: "app4",
				},
				State: node.ProcessStateRunning,
			},
		},
	})
	assert.Nil(t, err)

	mockDriver.EXPECT().StartProcess(
		gomock.Eq(
			&node.Process{
				ID:    "proc1",
				AppID: "app1",
			},
		),
		gomock.Eq(
			&node.AppInstallation{
				ID:   "app1",
				Name: "appname1",
				Package: node.Package{
					Driver: "test",
				},
			},
		),
	).Return("ext_proc1", nil).Times(1)

	mockDriver.EXPECT().StartProcess(
		gomock.Eq(
			&node.Process{
				ID:    "proc2",
				AppID: "app2",
			},
		),
		gomock.Eq(
			&node.AppInstallation{
				ID:   "app2",
				Name: "appname2",
				Package: node.Package{
					Driver: "test",
				},
			},
		),
	).Return("ext_proc1", nil).Times(1)

	mockDriver.EXPECT().StartProcess(
		gomock.Eq(
			&node.Process{
				ID:    "proc3",
				AppID: "app3",
			},
		),
		gomock.Eq(
			&node.AppInstallation{
				ID:   "app3",
				Name: "appname3",
				Package: node.Package{
					Driver: "test",
				},
			},
		),
	).Return("", errors.New("Error starting process")).Times(1)

	mockDriver.EXPECT().StartProcess(
		gomock.Eq(
			&node.Process{
				ID:    "proc4",
				AppID: "app4",
			},
		),
		gomock.Eq(
			&node.AppInstallation{
				ID:   "app4",
				Name: "appname4",
				Package: node.Package{
					Driver: "test",
				},
			},
		),
	).Return("", errors.New("Error starting process")).Times(1)

	nc.EXPECT().SetProcessRunning("proc2").Return(nil).Times(1)

	err = pr.Restore(restoreUpdate)
	time.Sleep(100 * time.Millisecond)
	assert.Nil(t, err)

}
