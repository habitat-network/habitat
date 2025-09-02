package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/api"
	"github.com/eagraf/habitat-new/internal/node/api/test_helpers"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"
)

// mock process driver for tests
type entry struct {
	isStart bool
	id      node.ProcessID
}

type mockDriver struct {
	returnErr error
	log       []entry
	running   map[node.ProcessID]struct{} // presence indicates process is running
	name      node.DriverType
}

var _ process.Driver = &mockDriver{}

func newMockDriver(driver node.DriverType) *mockDriver {
	return &mockDriver{
		name:    driver,
		running: make(map[node.ProcessID]struct{}),
	}
}

func (d *mockDriver) Type() node.DriverType {
	return d.name
}

func fakeProcessManager() process.ProcessManager {
	mockDriver := newMockDriver(node.DriverTypeDocker)
	return process.NewProcessManager([]process.Driver{mockDriver})
}

func (d *mockDriver) StartProcess(ctx context.Context, processID node.ProcessID, app *node.AppInstallation) error {
	if d.returnErr != nil {
		return d.returnErr
	}
	d.log = append(d.log, entry{isStart: true, id: processID})
	d.running[processID] = struct{}{}
	return nil
}

func (d *mockDriver) StopProcess(ctx context.Context, processID node.ProcessID) error {
	if d.returnErr != nil {
		return d.returnErr
	}
	d.log = append(d.log, entry{isStart: false, id: processID})
	delete(d.running, processID)
	return nil
}

func (d *mockDriver) IsRunning(ctx context.Context, id node.ProcessID) (bool, error) {
	_, ok := d.running[id]
	return ok, nil
}

// Returns all running process or a non-nil error if that information cannot be extracted
func (d *mockDriver) ListRunningProcesses(context.Context) ([]node.ProcessID, error) {
	return maps.Keys(d.running), nil
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
	testPkg = &node.Package{
		Driver:       node.DriverTypeDocker,
		DriverConfig: map[string]interface{}{},
	}

	proc1 = &node.Process{
		ID:    "proc1",
		AppID: "app1",
	}
	proc2 = &node.Process{
		ID:    "proc2",
		AppID: "app2",
	}

	state = &node.State{
		SchemaVersion: node.LatestVersion,
		Users: map[string]*node.User{
			"user1": {
				ID: "user1",
			},
		},
		AppInstallations: map[string]*node.AppInstallation{
			"app1": {
				ID:      "app1",
				UserID:  "user_1",
				Name:    "appname1",
				State:   node.AppLifecycleStateInstalled,
				Package: testPkg,
			},
			"app2": {
				ID:      "app2",
				UserID:  "user_1",
				Name:    "appname2",
				State:   node.AppLifecycleStateInstalled,
				Package: testPkg,
			},
		},
		Processes:         map[node.ProcessID]*node.Process{},
		ReverseProxyRules: map[string]*node.ReverseProxyRule{},
	}
)

func TestStartProcessHandler(t *testing.T) {
	// For this test don't add any running processes to the initial state
	middleware := &test_helpers.TestAuthMiddleware{UserID: "user_1"}

	mockDriver := newMockDriver(node.DriverTypeDocker)
	db := &mockHDB{
		schema:    state.Schema(),
		jsonState: jsonStateFromNodeState(state),
	}

	// NewCtrlServer restores the initial state
	ctrl2, err := NewController(context.Background(), process.NewProcessManager([]process.Driver{mockDriver}), nil, db, nil, "fake-pds")
	require.NoError(t, err)

	s, err := NewCtrlServer(context.Background(), ctrl2, state)
	require.NoError(t, err)

	// No processes running
	procs, err := s.inner.processManager.ListRunningProcesses(context.Background())
	require.NoError(t, err)
	require.Len(t, procs, 0)

	startProcessHandler := http.HandlerFunc(s.StartProcess)
	startProcessRoute := api.NewBasicRoute(http.MethodPost, "/node/processes", startProcessHandler)
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
		httptest.NewRequest(
			startProcessRoute.Method(),
			startProcessRoute.Pattern(),
			bytes.NewReader(b),
		))
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
		httptest.NewRequest(
			startProcessRoute.Method(),
			startProcessRoute.Pattern(),
			bytes.NewReader(b),
		))
	require.Equal(t, http.StatusInternalServerError, resp.Result().StatusCode)

	// Test invalid request
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			startProcessRoute.Method(),
			startProcessRoute.Pattern(),
			bytes.NewReader([]byte("invalid")),
		),
	)
	assert.Equal(t, http.StatusBadRequest, resp.Result().StatusCode)

	// Process exists in node state
	nodeState := nodeStateFromJSONState(db.jsonState)
	procsForUser, err := nodeState.GetProcessesForUser("user_1")
	require.NoError(t, err)
	require.Len(t, procsForUser, 1)

	// Process manager has it
	id := procsForUser[0].ID
	running, err := s.inner.processManager.IsRunning(context.Background(), id)
	require.NoError(t, err)
	require.True(t, running)

	listFn := http.HandlerFunc(s.ListProcesses)
	listProcessRoute := api.NewBasicRoute(http.MethodGet, "/node/processes/list", listFn)
	resp = httptest.NewRecorder()
	handler = middleware.Middleware(listFn)
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			http.MethodGet,
			listProcessRoute.Pattern(),
			nil,
		),
	)

	var listed []node.ProcessID
	err = json.Unmarshal(resp.Body.Bytes(), &listed)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, listed[0], id)
	require.Equal(t, http.StatusOK, resp.Result().StatusCode)

	// Test Stop Process
	stopFn := http.HandlerFunc(s.StopProcess)
	handler = middleware.Middleware(stopFn)

	// Sad Path
	mockDriver.returnErr = fmt.Errorf("my error")
	b, err = json.Marshal(&StopProcessRequest{
		ProcessID: id,
	})
	require.NoError(t, err)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			http.MethodGet,
			"/node/processes/stop",
			bytes.NewReader(b),
		),
	)
	assert.Equal(t, http.StatusInternalServerError, resp.Result().StatusCode)

	// Happy Path
	mockDriver.returnErr = nil
	b, err = json.Marshal(&StopProcessRequest{
		ProcessID: id,
	})
	require.NoError(t, err)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			http.MethodGet,
			"/node/processes/stop",
			bytes.NewReader(b),
		),
	)
	assert.Equal(t, http.StatusOK, resp.Result().StatusCode)

	// Test invalid request
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			http.MethodPost,
			"/node/processes/stop",
			bytes.NewReader([]byte("invalid")),
		),
	)
	assert.Equal(t, http.StatusBadRequest, resp.Result().StatusCode)

	// Process no longer exists in node state
	nodeState = nodeStateFromJSONState(db.jsonState)
	procsForUser, err = nodeState.GetProcessesForUser("user_1")
	require.NoError(t, err)
	require.Len(t, procsForUser, 0)

	// Deleted from process manager
	running, err = s.inner.processManager.IsRunning(context.Background(), id)
	require.NoError(t, err)
	require.False(t, running)

	procs, err = s.inner.processManager.ListRunningProcesses(context.Background())
	require.NoError(t, err)
	require.Len(t, procs, 0)

	// Non-existent process
	b, err = json.Marshal(&StopProcessRequest{
		ProcessID: "docker:fake-id",
	})
	require.NoError(t, err)
	resp = httptest.NewRecorder()
	handler.ServeHTTP(
		resp,
		httptest.NewRequest(
			http.MethodGet,
			"/node/processes/stop",
			bytes.NewReader(b),
		),
	)

	assert.Equal(t, http.StatusInternalServerError, resp.Result().StatusCode)

	// Also test the inner error we get
	require.ErrorContains(t, s.inner.stopProcess("docker:fake-id"), "process with id not found: docker:fake-id")
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

	mockDriver := newMockDriver(node.DriverTypeDocker)
	pm := process.NewProcessManager([]process.Driver{mockDriver})

	ctrl, err := NewController(context.Background(), pm, nil, &mockHDB{
		schema:    state.Schema(),
		jsonState: jsonStateFromNodeState(state),
	},
		nil,
		"fake-pds",
	)
	require.NoError(t, err)

	// Restore
	require.NoError(t, ctrl.restore(state))

	procs, err := ctrl.processManager.ListRunningProcesses(context.Background())
	require.NoError(t, err)

	// Sort by procID so we can assert on the states
	require.Len(t, procs, 2)
	slices.SortFunc(procs, func(a, b node.ProcessID) int {
		return strings.Compare(string(a), string(b))
	})

	// Ensure processManager has expected state
	require.Equal(t, procs[0], proc1.ID)
	require.Equal(t, procs[1], proc2.ID)
}
