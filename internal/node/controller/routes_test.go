package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	types "github.com/eagraf/habitat-new/core/api"
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/controller/mocks"
	hdb_mocks "github.com/eagraf/habitat-new/internal/node/hdb/mocks"
	"github.com/eagraf/habitat-new/internal/package_manager"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestMigrations(t *testing.T) {
	ctrl := gomock.NewController(t)

	m := mocks.NewMockNodeController(ctrl)
	handler := NewMigrationRoute(m)

	b, err := json.Marshal(types.MigrateRequest{
		TargetVersion: "v0.0.2",
	})
	require.NoError(t, err)

	m.EXPECT().MigrateNodeDB("v0.0.2").Return(nil).Times(1)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, handler.Pattern(), bytes.NewReader(b)),
	)
	require.Equal(t, http.StatusOK, resp.Result().StatusCode)

	// Test error from node controller
	m.EXPECT().MigrateNodeDB("v0.0.2").Return(errors.New("invalid version")).Times(1)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, handler.Pattern(), bytes.NewReader(b)),
	)
	require.Equal(t, http.StatusInternalServerError, resp.Result().StatusCode)

	// Test invalid semver
	b, err = json.Marshal(types.MigrateRequest{
		TargetVersion: "invalid",
	})
	require.NoError(t, err)

	m.EXPECT().MigrateNodeDB(gomock.Any()).Times(0)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, handler.Pattern(), bytes.NewReader(b)),
	)
	assert.Equal(t, http.StatusBadRequest, resp.Result().StatusCode)

}

func TestGetNodeHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockDB := hdb_mocks.NewMockHDBManager(ctrl)
	mockClient := hdb_mocks.NewMockClient(ctrl)

	// Note: not using generateInitState() to test
	testState := map[string]interface{}{
		"state_prop":   "state_val",
		"state_prop_2": "state_val_2",
		// "number":       int(1), <-- this does not pass because the unmarshal after writing decodes number as float64 by default
		"bool":  true,
		"float": 1.65,
	}
	bytes, err := json.Marshal(testState)
	require.Nil(t, err)

	mockDB.EXPECT().GetDatabaseClientByName(constants.NodeDBDefaultName).Return(mockClient, nil)
	mockClient.EXPECT().Bytes().Return(bytes)
	mockClient.EXPECT().DatabaseID().Return(uuid.New().String())

	handler := NewGetNodeRoute(mockDB)

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, handler.Pattern(), nil))
	require.Equal(t, http.StatusOK, resp.Result().StatusCode)

	bytes, err = io.ReadAll(resp.Result().Body)
	require.NoError(t, err)

	var respBody types.GetNodeResponse
	require.NoError(t, json.Unmarshal(bytes, &respBody))
}

func TestAddUserHandler(t *testing.T) {
	ctrl := gomock.NewController(t)

	m := mocks.NewMockNodeController(ctrl)
	handler := NewAddUserRoute(m)

	b, err := json.Marshal(&types.PostAddUserRequest{
		UserID:      "myUserID",
		Email:       "user@user.com",
		Handle:      "myUsername",
		Password:    "password",
		Certificate: "myCert",
	})
	require.NoError(t, err)

	m.EXPECT().AddUser("myUserID", "user@user.com", "myUsername", "password", "myCert").Return(map[string]interface{}{}, nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, handler.Pattern(), bytes.NewReader(b)),
	)
	require.Equal(t, http.StatusCreated, resp.Result().StatusCode)

	// Test internal server error
	m.EXPECT().AddUser("myUserID", "user@user.com", "myUsername", "password", "myCert").Return(map[string]interface{}{}, errors.New("error adding user"))
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, handler.Pattern(), bytes.NewReader(b)),
	)
	require.Equal(t, http.StatusInternalServerError, resp.Result().StatusCode)

	// Test invalid request
	m.EXPECT().AddUser("myUserID", "user@user.com", "myUsername", "password", "myCert").Times(0)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, handler.Pattern(), bytes.NewReader([]byte("invalid"))),
	)
	require.Equal(t, http.StatusBadRequest, resp.Result().StatusCode)
}

func TestLogin(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockPDS := mocks.NewMockPDSClientI(ctrl)

	handler := NewLoginRoute(mockPDS)

	mockPDS.EXPECT().CreateSession("identifier", "password").Return(types.PDSCreateSessionResponse{}, nil).Times(1)

	b, err := json.Marshal(types.PDSCreateSessionRequest{
		Identifier: "identifier",
		Password:   "password",
	})
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, handler.Pattern(), bytes.NewReader(b)),
	)
	require.Equal(t, http.StatusOK, resp.Result().StatusCode)

}

func TestGetNodeState(t *testing.T) {
	mockDriver := newMockDriver(node.DriverTypeDocker)
	ctrl2, err := NewController2(context.Background(), process.NewProcessManager([]process.Driver{mockDriver}),
		map[node.DriverType]package_manager.PackageManager{
			node.DriverTypeDocker: &mockPkgManager{
				installs: make(map[*node.Package]struct{}),
			},
		},
		&mockHDB{
			schema:    state.Schema(),
			jsonState: jsonStateFromNodeState(state),
		}, nil)
	require.NoError(t, err)
	ctrlServer, err := NewCtrlServer(
		context.Background(),
		&BaseNodeController{},
		ctrl2,
		state,
	)
	require.NoError(t, err)

	handler := http.HandlerFunc(ctrlServer.GetNodeState)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, httptest.NewRequest("get", "/test", nil))
	bytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	sb, err := state.Bytes()
	require.NoError(t, err)
	require.Equal(t, bytes, sb)
}
