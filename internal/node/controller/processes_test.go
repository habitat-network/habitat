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

	"github.com/eagraf/habitat-new/internal/node/api"
	node_state "github.com/eagraf/habitat-new/internal/node/state"

	"github.com/eagraf/habitat-new/internal/node/api/test_helpers"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"
)

// mock process driver for tests
type entry struct {
	isStart bool
	id      node_state.ProcessID
}

type mockDriver struct {
	returnErr error
	log       []entry
	running   map[node_state.ProcessID]struct{} // presence indicates process is running
	name      node_state.DriverType
}

var _ process.Driver = &mockDriver{}

func newMockDriver(driver node_state.DriverType) *mockDriver {
	return &mockDriver{
		name:    driver,
		running: make(map[node_state.ProcessID]struct{}),
	}
}

func (d *mockDriver) Type() node_state.DriverType {
	return d.name
}

func fakeProcessManager() process.ProcessManager {
	mockDriver := newMockDriver(node_state.DriverTypeDocker)
	return process.NewProcessManager([]process.Driver{mockDriver})
}

func (d *mockDriver) StartProcess(ctx context.Context, processID node_state.ProcessID, app *node_state.AppInstallation) error {
	if d.returnErr != nil {
		return d.returnErr
	}
	d.log = append(d.log, entry{isStart: true, id: processID})
	d.running[processID] = struct{}{}
	return nil
}

func (d *mockDriver) StopProcess(ctx context.Context, processID node_state.ProcessID) error {
	if d.returnErr != nil {
		return d.returnErr
	}
	d.log = append(d.log, entry{isStart: false, id: processID})
	delete(d.running, processID)
	return nil
}

func (d *mockDriver) IsRunning(ctx context.Context, id node_state.ProcessID) (bool, error) {
	_, ok := d.running[id]
	return ok, nil
}

// Returns all running process or a non-nil error if that information cannot be extracted
func (d *mockDriver) ListRunningProcesses(context.Context) ([]node_state.ProcessID, error) {
	return maps.Keys(d.running), nil
}

var (
	proc1 = &node_state.Process{
		ID:    "proc1",
		AppID: "app1",
	}
	proc2 = &node_state.Process{
		ID:    "proc2",
		AppID: "app2",
	}
)

func fakeState() *node_state.NodeState {
	testPkg := &node_state.Package{
		Driver:       node_state.DriverTypeDocker,
		DriverConfig: map[string]interface{}{},
	}

	return &node_state.NodeState{
		SchemaVersion: node_state.LatestVersion,
		Users: map[string]*node_state.User{
			"user1": {
				ID: "user1",
			},
		},
		AppInstallations: map[string]*node_state.AppInstallation{
			"app1": {
				ID:      "app1",
				UserID:  "user1",
				Name:    "appname1",
				State:   node_state.AppLifecycleStateInstalled,
				Package: testPkg,
			},
			"app2": {
				ID:      "app2",
				UserID:  "user1",
				Name:    "appname2",
				State:   node_state.AppLifecycleStateInstalled,
				Package: testPkg,
			},
		},
		Processes:         map[node_state.ProcessID]*node_state.Process{},
		ReverseProxyRules: map[string]*node_state.ReverseProxyRule{},
	}
}

func TestStartProcessHandler(t *testing.T) {
	// For this test don't add any running processes to the initial state
	middleware := &test_helpers.TestAuthMiddleware{UserID: "user1"}

	mockDriver := newMockDriver(node_state.DriverTypeDocker)
	state := fakeState()
	db := testDB(state)

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
	nodeState, err := db.State()
	require.NoError(t, err)
	procsForUser, err := nodeState.GetProcessesForUser("user1")
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

	var listed []node_state.ProcessID
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
	nodeState, err = db.State()
	require.NoError(t, err)
	procsForUser, err = nodeState.GetProcessesForUser("user1")
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
	require.ErrorContains(t, s.inner.stopProcess("docker:fake-id"), "process with id docker:fake-id not found")
}

func TestControllerRestoreProcess(t *testing.T) {
	state := fakeState()
	// For this test, initial state should have Started processees for restore
	state.Processes["proc1"] = proc1
	state.Processes["proc2"] = proc2

	mockDriver := newMockDriver(node_state.DriverTypeDocker)
	pm := process.NewProcessManager([]process.Driver{mockDriver})
	db := testDB(state)

	ctrl, err := NewController(context.Background(), pm, nil, db, nil, "fake-pds")
	require.NoError(t, err)

	// Restore
	require.NoError(t, ctrl.restore(state))

	procs, err := ctrl.processManager.ListRunningProcesses(context.Background())
	require.NoError(t, err)

	// Sort by procID so we can assert on the states
	require.Len(t, procs, 2)
	slices.SortFunc(procs, func(a, b node_state.ProcessID) int {
		return strings.Compare(string(a), string(b))
	})

	// Ensure processManager has expected state
	require.Equal(t, procs[0], proc1.ID)
	require.Equal(t, procs[1], proc2.ID)
}
