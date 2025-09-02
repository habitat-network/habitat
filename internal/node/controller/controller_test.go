package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	hdb_mocks "github.com/eagraf/habitat-new/internal/node/hdb/mocks"
	"github.com/eagraf/habitat-new/internal/package_manager"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func fakeInitState(rootUserCert, rootUserID, rootUsername string) *node.State {
	initState, err := node.GetEmptyStateForVersion(node.LatestVersion)
	if err != nil {
		panic(err)
	}

	initState.Users[rootUserID] = &node.User{
		ID:       rootUserID,
		Username: rootUsername,
	}

	return initState
}

func setupNodeDBTest(ctrl *gomock.Controller, t *testing.T) *hdb_mocks.MockClient {
	mockedClient := hdb_mocks.NewMockClient(ctrl)

	initState := fakeInitState("fake_cert", "fake_user_id", "fake_username")
	transitions := []hdb.Transition{
		node.CreateInitializationTransition(initState),
	}

	mockedClient.EXPECT().ProposeTransitions(transitions)
	_, err := mockedClient.ProposeTransitions(transitions)
	if err != nil {
		t.Fatal(err)
	}
	return mockedClient
}

func TestAddUser(t *testing.T) {
	ctrl := gomock.NewController(t)

	mockedClient := setupNodeDBTest(ctrl, t)

	did := "did"
	mockPDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/com.atproto.server.createAccount", r.URL.String())
		bytes, err := json.Marshal(
			atproto.ServerCreateAccount_Output{
				Did:    did,
				Handle: "user-handle",
			},
		)
		require.NoError(t, err)
		_, err = w.Write(bytes)
		require.NoError(t, err)
	}))
	defer mockPDS.Close()

	ctrl2, err := NewController(context.Background(), fakeProcessManager(), nil, mockedClient, nil, mockPDS.URL)
	require.NoError(t, err)

	mockedClient.EXPECT().ProposeTransitions(gomock.Any()).Return(nil, nil).Times(1)

	email := "user@user.com"
	pass := "pass"
	input := &atproto.ServerCreateAccount_Input{
		Did:      &did,
		Email:    &email,
		Handle:   "user-handle",
		Password: &pass,
	}

	out, err := ctrl2.addUser(context.Background(), input)
	require.Nil(t, err)
	require.Equal(t, out.Did, did)
}

func TestMigrations(t *testing.T) {
	fakestate := &node.State{
		SchemaVersion:     "v0.0.1",
		Users:             map[string]*node.User{},
		AppInstallations:  map[string]*node.AppInstallation{},
		Processes:         map[node.ProcessID]*node.Process{},
		ReverseProxyRules: map[string]*node.ReverseProxyRule{},
	}
	db := &mockHDB{
		schema:    fakestate.Schema(),
		jsonState: jsonStateFromNodeState(fakestate),
	}
	ctrl2, err := NewController(context.Background(), fakeProcessManager(), nil, db, nil, "fak-pds")
	require.NoError(t, err)
	s, err := NewCtrlServer(context.Background(), ctrl2, fakestate)
	require.NoError(t, err)
	handler := http.HandlerFunc(s.MigrateDB)

	b, err := json.Marshal(MigrateRequest{
		TargetVersion: "v0.0.2",
	})
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, "/doesntmatter", bytes.NewReader(b)),
	)
	require.Equal(t, http.StatusOK, resp.Result().StatusCode)
}

func TestGetNodeState(t *testing.T) {
	ctrl2, err := NewController(context.Background(), fakeProcessManager(),
		map[node.DriverType]package_manager.PackageManager{
			node.DriverTypeDocker: &mockPkgManager{
				installs: make(map[*node.Package]struct{}),
			},
		},
		&mockHDB{
			schema:    state.Schema(),
			jsonState: jsonStateFromNodeState(state),
		}, nil, "fake-pds")
	require.NoError(t, err)
	ctrlServer, err := NewCtrlServer(
		context.Background(),
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
