package controller

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/controller/mocks"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	hdb_mocks "github.com/eagraf/habitat-new/internal/node/hdb/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func setupNodeDBTest(ctrl *gomock.Controller, t *testing.T) (NodeController, *mocks.MockPDSClientI, *hdb_mocks.MockHDBManager, *hdb_mocks.MockClient) {

	mockedPDSClient := mocks.NewMockPDSClientI(ctrl)
	mockedManager := hdb_mocks.NewMockHDBManager(ctrl)
	mockedClient := hdb_mocks.NewMockClient(ctrl)

	// Check that fakeInitState is based off of the config we pass in
	mockedManager.EXPECT().CreateDatabase(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		// signature of anonymous function must have the same number of input and output arguments as the mocked method.
		func(nodeName, schemaName string, initTransitions []hdb.Transition) (hdb.Client, error) {
			require.Equal(t, 4, len(initTransitions))

			initStateTransition := initTransitions[0]
			require.Equal(t, node.TransitionInitialize, initStateTransition.Type())

			state := initStateTransition.(*node.InitalizationTransition).InitState

			assert.Equal(t, 1, len(state.Users))
			assert.Equal(t, constants.RootUsername, state.Users["0"].Username)
			assert.Equal(t, constants.RootUserID, state.Users["0"].ID)
			assert.Equal(t, "cm9vdF9jZXJ0", state.Users["0"].Certificate)

			return mockedClient, nil
		}).Times(1)

	config, err := config.NewTestNodeConfig(nil)
	require.Nil(t, err)

	controller, err := NewNodeController(mockedManager, config)
	controller.pdsClient = mockedPDSClient
	require.Nil(t, err)
	err = controller.InitializeNodeDB()
	require.Nil(t, err)

	return controller, mockedPDSClient, mockedManager, mockedClient
}

func TestInitializeNodeDB(t *testing.T) {
	ctrl := gomock.NewController(t)

	controller, _, mockedManager, _ := setupNodeDBTest(ctrl, t)

	mockedManager.EXPECT().CreateDatabase(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, assert.AnError).Times(1)
	err := controller.InitializeNodeDB()
	require.NotNil(t, err)

	mockedManager.EXPECT().CreateDatabase(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, &hdb.DatabaseAlreadyExistsError{}).Times(1)
	err = controller.InitializeNodeDB()
	require.Nil(t, err)
}

func TestMigrationController(t *testing.T) {
	ctrl := gomock.NewController(t)

	controller, _, mockedManager, mockClient := setupNodeDBTest(ctrl, t)

	nodeState := &node.State{
		SchemaVersion: "v0.0.2",
	}
	marshaled, err := nodeState.Bytes()
	assert.Equal(t, nil, err)

	mockedManager.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockClient, nil).Times(1)
	mockClient.EXPECT().Bytes().Return(marshaled).Times(1)
	mockClient.EXPECT().ProposeTransitions(gomock.Eq(
		[]hdb.Transition{
			&node.MigrationTransition{
				TargetVersion: "v0.0.3",
			},
		},
	)).Return(nil, nil).Times(1)

	err = controller.MigrateNodeDB("v0.0.3")
	assert.Nil(t, err)

	// Test no-op by migrating to the same version
	mockedManager.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockClient, nil).Times(1)
	mockClient.EXPECT().Bytes().Return(marshaled).Times(1)
	mockClient.EXPECT().ProposeTransitions(gomock.Any()).Times(0)

	err = controller.MigrateNodeDB("v0.0.2")
	assert.Nil(t, err)

	// Test  migrating to a lower version

	mockedManager.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockClient, nil).Times(1)
	mockClient.EXPECT().Bytes().Return(marshaled).Times(1)
	mockClient.EXPECT().ProposeTransitions(gomock.Eq(
		[]hdb.Transition{
			&node.MigrationTransition{
				TargetVersion: "v0.0.1",
			},
		},
	)).Times(1)

	err = controller.MigrateNodeDB("v0.0.1")
	assert.Nil(t, err)
}

func TestInstallAppController(t *testing.T) {
	ctrl := gomock.NewController(t)

	controller, _, mockedManager, mockedClient := setupNodeDBTest(ctrl, t)

	mockedManager.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockedClient, nil).Times(1)
	mockedClient.EXPECT().ProposeTransitions(gomock.Eq(
		[]hdb.Transition{
			&node.StartInstallationTransition{
				UserID: "0",
				AppInstallation: &node.AppInstallation{
					Name:    "app_name1",
					Version: "1",
					Package: node.Package{
						Driver:             "docker",
						RegistryURLBase:    "https://registry.com",
						RegistryPackageID:  "app_name1",
						RegistryPackageTag: "v1",
					},
				},
				NewProxyRules: []*node.ReverseProxyRule{},
			},
		},
	)).Return(nil, nil).Times(1)

	err := controller.InstallApp("0", &node.AppInstallation{
		Name:    "app_name1",
		Version: "1",
		Package: node.Package{
			Driver:             "docker",
			RegistryURLBase:    "https://registry.com",
			RegistryPackageID:  "app_name1",
			RegistryPackageTag: "v1",
		},
	}, []*node.ReverseProxyRule{})
	assert.Nil(t, err)
}

var nodeState = &node.State{
	Users: map[string]*node.User{
		"user_1": {
			ID:       "user_1",
			Username: "username_1",
		},
	},
	AppInstallations: map[string]*node.AppInstallationState{
		"app_1": {
			AppInstallation: &node.AppInstallation{
				ID:     "app_1",
				UserID: "0",
			},
			State: node.AppLifecycleStateInstalled,
		},
	},
}

func TestFinishAppInstallationController(t *testing.T) {
	ctrl := gomock.NewController(t)

	controller, _, mockedManager, mockedClient := setupNodeDBTest(ctrl, t)

	mockedManager.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockedClient, nil).Times(1)
	mockedClient.EXPECT().ProposeTransitions(gomock.Eq(
		[]hdb.Transition{
			&node.FinishInstallationTransition{
				AppID:           "app1",
				UserID:          "user_1",
				RegistryURLBase: "https://registry.com",
				RegistryAppID:   "app_1",
			},
		},
	)).Return(nil, nil).Times(1)

	err := controller.FinishAppInstallation("user_1", "app1", "https://registry.com", "app_1", false)
	assert.Nil(t, err)
}

func TestGetAppByID(t *testing.T) {
	ctrl := gomock.NewController(t)

	controller, _, mockedManager, mockedClient := setupNodeDBTest(ctrl, t)

	marshaledNodeState, err := json.Marshal(nodeState)
	if err != nil {
		t.Error(err)
	}

	mockedClient.EXPECT().Bytes().Return(marshaledNodeState).Times(2)

	mockedManager.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockedClient, nil).Times(2)

	app, err := controller.GetAppByID("app_1")
	assert.Nil(t, err)
	assert.Equal(t, "app_1", app.ID)

	// Test username not found
	app, err = controller.GetAppByID("app_2")
	assert.NotNil(t, err)
	assert.Nil(t, app)
}

func TestStartProcessController(t *testing.T) {
	ctrl := gomock.NewController(t)

	controller, _, mockedManager, mockedClient := setupNodeDBTest(ctrl, t)

	marshaledNodeState, err := json.Marshal(nodeState)
	if err != nil {
		t.Error(err)
	}

	mockedClient.EXPECT().Bytes().Return(marshaledNodeState).Times(1)

	mockedManager.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockedClient, nil).Times(3)
	mockedClient.EXPECT().ProposeTransitions(gomock.Eq(
		[]hdb.Transition{
			&node.ProcessStartTransition{
				AppID: "app_1",
			},
		},
	)).Return(nil, nil).Times(1)
	mockedClient.EXPECT().ProposeTransitions(gomock.Eq(
		[]hdb.Transition{
			&node.ProcessRunningTransition{
				ProcessID: "process_1",
			},
		},
	)).Return(nil, nil).Times(1)

	mockedClient.EXPECT().ProposeTransitions(gomock.Eq(
		[]hdb.Transition{
			&node.ProcessStopTransition{
				ProcessID: "process_1",
			},
		},
	)).Return(nil, nil).Times(1)

	err = controller.StartProcess("app_1")
	assert.Nil(t, err)

	// Test setting the process to running, and then stopping it.
	err = controller.SetProcessRunning("process_1")
	assert.Nil(t, err)

	err = controller.StopProcess("process_1")
	assert.Nil(t, err)
}

func TestAddUser(t *testing.T) {
	ctrl := gomock.NewController(t)

	controller, mockedPDSClient, mockedManager, mockedClient := setupNodeDBTest(ctrl, t)

	// Test successful add user

	mockedPDSClient.EXPECT().GetInviteCode(gomock.Any()).Return("invite_code", nil).Times(1)

	mockedPDSClient.EXPECT().CreateAccount(gomock.Any(), "user@user.com", "username_1", "password", "invite_code").Return(map[string]interface{}{
		"did": "did_1",
	}, nil).Times(1)

	mockedManager.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockedClient, nil).Times(1)
	mockedClient.EXPECT().ProposeTransitions(gomock.Eq(
		[]hdb.Transition{
			&node.AddUserTransition{
				Username:    "username_1",
				Certificate: "cert_1",
				AtprotoDID:  "did_1",
			},
		},
	)).Return(nil, nil).Times(1)

	_, err := controller.AddUser("user_1", "user@user.com", "username_1", "password", "cert_1")
	assert.Nil(t, err)

	// Test error from empty did.
	mockedPDSClient.EXPECT().GetInviteCode(gomock.Any()).Return("invite_code", nil).Times(1)

	mockedPDSClient.EXPECT().CreateAccount(gomock.Any(), "user@user.com", "username_1", "password", "invite_code").Return(map[string]interface{}{
		"did": "",
	}, nil).Times(1)

	_, err = controller.AddUser("user_1", "user@user.com", "username_1", "password", "cert_1")
	assert.NotNil(t, err)

	// Test error from missing did
	mockedPDSClient.EXPECT().GetInviteCode(gomock.Any()).Return("invite_code", nil).Times(1)

	mockedPDSClient.EXPECT().CreateAccount(gomock.Any(), "user@user.com", "username_1", "password", "invite_code").Return(map[string]interface{}{}, nil).Times(1)

	_, err = controller.AddUser("user_1", "user@user.com", "username_1", "password", "cert_1")
	assert.NotNil(t, err)

	// Test invite code error.
	mockedPDSClient.EXPECT().GetInviteCode(gomock.Any()).Return("", errors.New("failed to create invite code")).Times(1)
	_, err = controller.AddUser("user_1", "user@user.com", "username_1", "password", "cert_1")
	assert.NotNil(t, err)

	// Test create account error
	mockedPDSClient.EXPECT().GetInviteCode(gomock.Any()).Return("invite_code", nil).Times(1)

	mockedPDSClient.EXPECT().CreateAccount(gomock.Any(), "user@user.com", "username_1", "password", "invite_code").Return(nil, errors.New("failed to create account")).Times(1)

	_, err = controller.AddUser("user_1", "user@user.com", "username_1", "password", "cert_1")
	assert.NotNil(t, err)

}

func TestGetUserByUsername(t *testing.T) {
	ctrl := gomock.NewController(t)

	controller, _, mockedManager, mockedClient := setupNodeDBTest(ctrl, t)

	marshaledNodeState, err := json.Marshal(nodeState)
	if err != nil {
		t.Error(err)
	}

	mockedClient.EXPECT().Bytes().Return(marshaledNodeState).Times(2)

	mockedManager.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockedClient, nil).Times(2)

	user, err := controller.GetUserByUsername("username_1")
	assert.Nil(t, err)
	assert.Equal(t, "user_1", user.ID)
	assert.Equal(t, "username_1", user.Username)

	// Test username not found
	user, err = controller.GetUserByUsername("username_2")
	assert.NotNil(t, err)
	assert.Nil(t, user)
}
