package process

import (
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/core/state/node/test_helpers"
	controller_mocks "github.com/eagraf/habitat-new/internal/node/controller/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSubscriber(t *testing.T) {

	mockDriver := newMockDriver()
	pm := NewProcessManager([]Driver{mockDriver})

	ctrl := gomock.NewController(t)
	nc := controller_mocks.NewMockNodeController(ctrl)

	startProcessExecutor := &StartProcessExecutor{
		processManager: pm,
		nodeController: nc,
	}

	startProcessStateUpdate, err := test_helpers.StateUpdateTestHelper(&node.ProcessStartTransition{
		AppID: "app1",
	}, &node.State{
		AppInstallations: map[string]*node.AppInstallationState{
			"app1": {
				AppInstallation: &node.AppInstallation{
					UserID: "0",
					ID:     "app1",
					Package: node.Package{
						Driver: mockDriver.Type(),
					},
				},
			},
		},
		Processes: map[string]*node.ProcessState{},
	})
	require.Nil(t, err)
	nc.EXPECT().SetProcessRunning(gomock.Any()).Times(1)

	shouldExecute, err := startProcessExecutor.ShouldExecute(startProcessStateUpdate)
	require.Nil(t, err)
	require.Equal(t, true, shouldExecute)

	err = startProcessExecutor.Execute(startProcessStateUpdate)
	require.Nil(t, err)

	shouldExecute, err = startProcessExecutor.ShouldExecute(startProcessStateUpdate)
	require.Nil(t, err)
	require.Equal(t, false, shouldExecute)

	require.Len(t, mockDriver.log, 1)
	for _, entry := range mockDriver.log {
		require.True(t, entry.isStart)
	}
}
