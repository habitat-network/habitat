package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eagraf/habitat-new/internal/app"
	"github.com/eagraf/habitat-new/internal/node/reverse_proxy"
	node_state "github.com/eagraf/habitat-new/internal/node/state"
	"github.com/eagraf/habitat-new/internal/process"

	"github.com/stretchr/testify/require"
)

func TestAddUser(t *testing.T) {
	initState, err := node_state.GetEmptyStateForVersion(node_state.LatestVersion)
	if err != nil {
		panic(err)
	}

	initState.Users["fake_user_id"] = &node_state.User{
		ID:       "fake_user_id",
		Username: "fake_username",
	}

	db := testDB(initState)

	ctrl2, err := NewController(context.Background(), fakeProcessManager(), nil, db, nil)
	require.NoError(t, err)

	err = ctrl2.addUser("user-handle", "did")
	require.Nil(t, err)
}

type fakeDB struct {
	state *node_state.NodeState
}

func (db *fakeDB) Bytes() (node_state.SerializedState, error) {
	return db.state.Bytes()
}

func (db *fakeDB) State() (*node_state.NodeState, error) {
	return db.state, nil
}

func (db *fakeDB) ProposeTransitions(transitions []node_state.Transition) (node_state.SerializedState, error) {
	old, err := db.state.Bytes()
	if err != nil {
		return nil, err
	}
	bytes, _, err := node_state.ProposeTransitions(old, transitions)
	if err != nil {
		return nil, err
	}

	var newState node_state.NodeState
	err = json.Unmarshal(bytes, &newState)
	if err != nil {
		return nil, err
	}

	db.state = &newState
	return bytes, err
}

func testDB(state *node_state.NodeState) node_state.Client {
	return &fakeDB{
		state: state,
	}
}

func TestMigrations(t *testing.T) {
	fakestate := &node_state.NodeState{
		SchemaVersion:     "v0.0.1",
		Users:             map[string]*node_state.User{},
		AppInstallations:  map[string]*app.Installation{},
		Processes:         map[process.ID]*process.Process{},
		ReverseProxyRules: map[string]*reverse_proxy.Rule{},
	}
	db := testDB(fakestate)

	ctrl2, err := NewController(context.Background(), fakeProcessManager(), nil, db, nil)
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
	state := fakeState()
	db := testDB(state)

	ctrl2, err := NewController(context.Background(), fakeProcessManager(),
		map[app.DriverType]app.PackageManager{
			app.DriverTypeDocker: &mockPkgManager{
				installs: make(map[*app.Package]struct{}),
			},
		},
		db, nil)
	require.NoError(t, err)
	ctrlServer, err := NewCtrlServer(
		context.Background(),
		ctrl2,
		fakeState(),
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
