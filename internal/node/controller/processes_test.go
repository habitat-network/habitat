package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/api/test_helpers"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mock process driver for tests
type entry struct {
	isStart bool
	id      node.ProcessID
}

type mockDriver struct {
	returnErr error
	log       []entry
	name      string
}

var _ process.Driver = &mockDriver{}

func newMockDriver(driver string) *mockDriver {
	return &mockDriver{
		name: driver,
	}
}

func (d *mockDriver) Type() string {
	return d.name
}

func (d *mockDriver) StartProcess(ctx context.Context, process *node.Process, app *node.AppInstallation) error {
	if d.returnErr != nil {
		return d.returnErr
	}
	id := uuid.New().String()
	d.log = append(d.log, entry{isStart: true, id: node.ProcessID(id)})
	return nil
}

func (d *mockDriver) StopProcess(ctx context.Context, processID node.ProcessID) error {
	d.log = append(d.log, entry{isStart: false, id: processID})
	return nil
}

// mock hdb for tests
type mockHDB struct {
	schema    hdb.Schema
	jsonState *hdb.JSONState
}

func (db *mockHDB) Bytes() []byte {
	return db.jsonState.Bytes()
}
func (db *mockHDB) DatabaseID() string {
	return "test"
}
func (db *mockHDB) ProposeTransitions(transitions []hdb.Transition) (*hdb.JSONState, error) {
	// Blindly apply all transitions
	var state hdb.SerializedState = db.jsonState.Bytes()
	for _, t := range transitions {
		patch, err := t.Patch(state)
		if err != nil {
			return nil, err
		}
		err = db.jsonState.ApplyPatch(patch)
		if err != nil {
			return nil, err
		}
	}
	return db.jsonState, nil
}

var (
	testPkg = node.Package{
		Driver:       "docker",
		DriverConfig: map[string]interface{}{},
	}

	proc1 = &node.Process{
		ID:     "proc1",
		AppID:  "app1",
		Driver: "docker",
	}
	proc2 = &node.Process{
		ID:     "proc2",
		AppID:  "app2",
		Driver: "docker",
	}

	state = &node.State{
		SchemaVersion: "v0.0.7",
		Users: map[string]*node.User{
			"user1": {
				ID: "user1",
			},
		},
		AppInstallations: map[string]*node.AppInstallationState{
			"app1": {
				AppInstallation: &node.AppInstallation{
					ID:      "app1",
					UserID:  "user_1",
					Name:    "appname1",
					Package: testPkg,
				},
				State: node.AppLifecycleStateInstalled,
			},
			"app2": {
				AppInstallation: &node.AppInstallation{
					ID:      "app2",
					UserID:  "user_1",
					Name:    "appname2",
					Package: testPkg,
				},
				State: node.AppLifecycleStateInstalled,
			},
		},
		Processes: map[node.ProcessID]*node.Process{},
	}
)

func TestStartProcessHandler(t *testing.T) {
	// For this test don't add any running processes to the initial state
	middleware := &test_helpers.TestAuthMiddleware{UserID: "user_1"}

	mockDriver := newMockDriver("docker")
	db := &mockHDB{
		schema:    state.Schema(),
		jsonState: jsonStateFromNodeState(state),
	}

	// NewCtrlServer restores the initial state
	s, err := NewCtrlServer(context.Background(), &BaseNodeController{}, process.NewProcessManager([]process.Driver{mockDriver}), db)
	require.NoError(t, err)

	// No processes running
	procs, err := s.inner.processManager.ListProcesses()
	require.NoError(t, err)
	require.Len(t, procs, 0)

	startProcessHandler := http.HandlerFunc(s.StartProcess)
	startProcessRoute := newRoute(http.MethodPost, "/node/processes", startProcessHandler)
	handler := middleware.Middleware(startProcessHandler)

	b, err := json.Marshal(StartProcessRequest{
		AppInstallationID: "app1",
	})
	if err != nil {
		t.Error(err)
	}

	// Test the happy path
	resp := httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, startProcessRoute.Pattern(), bytes.NewReader(b)),
	)
	require.Equal(t, http.StatusCreated, resp.Result().StatusCode)

	respBody, err := io.ReadAll(resp.Result().Body)
	require.NoError(t, err)
	require.Equal(t, 0, len(respBody))

	// Test an error returned by the controller
	b, err = json.Marshal(StartProcessRequest{
		AppInstallationID: "app3", // non-existent app installation
	})
	if err != nil {
		t.Error(err)
	}
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(http.MethodPost, startProcessRoute.Pattern(), bytes.NewReader(b)),
	)
	require.Equal(t, http.StatusInternalServerError, resp.Result().StatusCode)

	// Test invalid request
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			http.MethodPost,
			startProcessRoute.Pattern(),
			bytes.NewReader([]byte("invalid")),
		),
	)
	assert.Equal(t, http.StatusBadRequest, resp.Result().StatusCode)

	// Process exists in node state
	nodeState := nodeStateFromJSONState(db.jsonState)
	procs, err = nodeState.GetProcessesForUser("user_1")
	require.NoError(t, err)
	require.Len(t, procs, 1)

	// Process manager has it
	id := procs[0].ID
	proc, ok := s.inner.processManager.GetProcess(id)
	require.True(t, ok)
	require.Equal(t, proc.ID, id)
	require.Equal(t, proc.AppID, "app1")
	require.Equal(t, proc.UserID, "user_1")

	listProcessRoute := newRoute(http.MethodGet, "/node/processes/list", http.HandlerFunc(s.ListProcesses))
	resp = httptest.NewRecorder()
	handler = middleware.Middleware(listProcessRoute.fn)
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			http.MethodGet,
			listProcessRoute.Pattern(),
			nil,
		),
	)

	var listed []*node.Process
	err = json.Unmarshal(resp.Body.Bytes(), &listed)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, listed[0].ID, id)
	require.Equal(t, http.StatusOK, resp.Result().StatusCode)

	// Test Stop Process
	stopRoute := newRoute(http.MethodGet, "/node/processes/stop", http.HandlerFunc(s.StopProcess))
	handler = middleware.Middleware(stopRoute.fn)

	// Happy Path
	b, err = json.Marshal(&StopProcessRequest{
		ProcessID: string(id),
	})
	require.NoError(t, err)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			http.MethodGet,
			stopRoute.Pattern(),
			bytes.NewReader(b),
		),
	)
	assert.Equal(t, http.StatusOK, resp.Result().StatusCode)

	// Process no longer exists in node state
	nodeState = nodeStateFromJSONState(db.jsonState)
	procs, err = nodeState.GetProcessesForUser("user_1")
	require.NoError(t, err)
	require.Len(t, procs, 0)

	// Deleted from process manager
	_, ok = s.inner.processManager.GetProcess(id)
	require.False(t, ok)
	procs, err = s.inner.processManager.ListProcesses()
	require.NoError(t, err)
	require.Len(t, procs, 0)

	// Non-existent process
	b, err = json.Marshal(&StopProcessRequest{
		ProcessID: "fake-id",
	})
	require.NoError(t, err)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			http.MethodGet,
			stopRoute.Pattern(),
			bytes.NewReader(b),
		),
	)
	assert.Equal(t, http.StatusInternalServerError, resp.Result().StatusCode)

	// Also test the inner error we get
	require.ErrorContains(t, s.inner.stopProcess("fake-id"), "process fake-id not found")
}

// Kind of annoying helper to do some typing
// In the long term, types across packages should be more aligned so we don't have to do this
// For example, hdb.NewJSONState() could take in node.State, but right now that causes an import cycle
// Which leads me to believer that maybe NewJSONState() shouldn't be in the hdb package but somewhere else
// For now, just work with it
func jsonStateFromNodeState(s *node.State) *hdb.JSONState {
	bytes, err := s.Bytes()
	if err != nil {
		panic(err)
	}
	state, err := hdb.NewJSONState(state.Schema(), bytes)
	if err != nil {
		panic(err)
	}
	return state
}

func nodeStateFromJSONState(j *hdb.JSONState) *node.State {
	var s node.State
	err := json.Unmarshal(j.Bytes(), &s)
	if err != nil {
		panic(err)
	}
	return &s
}

func TestControllerRestoreProcess(t *testing.T) {
	// For this test, initial state should have Started processees for restore
	state.Processes["proc1"] = proc1
	state.Processes["proc2"] = proc2

	mockDriver := newMockDriver("docker")
	pm := process.NewProcessManager([]process.Driver{mockDriver})

	ctrl, err := newController2(
		context.Background(),
		pm, &mockHDB{
			schema:    state.Schema(),
			jsonState: jsonStateFromNodeState(state),
		})
	require.NoError(t, err)

	// Restore
	require.NoError(t, ctrl.restore(state))

	procs, err := ctrl.processManager.ListProcesses()
	require.NoError(t, err)

	// Sort by procID so we can assert on the states
	require.Len(t, procs, 2)
	slices.SortFunc(procs, func(a, b *node.Process) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})

	// Ensure processManager has expected state
	require.Equal(t, procs[0].ID, proc1.ID)
	require.Equal(t, procs[0].AppID, proc1.AppID)
	require.Equal(t, procs[0].Driver, "docker")

	require.Equal(t, procs[1].ID, proc2.ID)
	require.Equal(t, procs[1].AppID, proc2.AppID)
	require.Equal(t, procs[1].Driver, "docker")
}
