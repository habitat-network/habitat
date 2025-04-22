package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/api/test_helpers"
	"github.com/eagraf/habitat-new/internal/package_manager"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/stretchr/testify/require"
)

type mockPkgManager struct {
	installs map[*node.Package]struct{}
}

func (m *mockPkgManager) Driver() node.DriverType {
	return node.DriverTypeUnknown
}
func (m *mockPkgManager) IsInstalled(packageSpec *node.Package, version string) (bool, error) {
	_, ok := m.installs[packageSpec]
	return ok, nil
}
func (m *mockPkgManager) InstallPackage(packageSpec *node.Package, version string) error {
	m.installs[packageSpec] = struct{}{}
	return nil
}
func (m *mockPkgManager) UninstallPackage(packageSpec *node.Package, version string) error {
	delete(m.installs, packageSpec)
	return nil
}

func (m *mockPkgManager) RestoreFromState(context.Context, map[string]*node.AppInstallation) error {
	return nil
}

func TestInstallAppController(t *testing.T) {
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
		}, nil, "fake-pds")
	require.NoError(t, err)
	ctrlServer, err := NewCtrlServer(
		context.Background(),
		ctrl2,
		state,
	)
	require.NoError(t, err)

	pkg := &node.Package{
		DriverConfig:       make(map[string]interface{}),
		Driver:             node.DriverTypeDocker,
		RegistryURLBase:    "https://registry.com",
		RegistryPackageID:  "app_name1",
		RegistryPackageTag: "v1",
	}

	// Same thing but with the server
	middleware := &test_helpers.TestAuthMiddleware{UserID: "user_1"}
	handler := middleware.Middleware(http.HandlerFunc(ctrlServer.InstallApp))
	resp := httptest.NewRecorder()
	b, err := json.Marshal(&InstallAppRequest{
		AppInstallation: &node.AppInstallation{
			Name:    "app_name1",
			Version: "1",
			Package: pkg,
		},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(
		http.MethodPost,
		`/install-app/users/{user_id}`, // fake path for tests
		bytes.NewReader(b),
	)
	req.SetPathValue("user_id", "user1")

	handler.ServeHTTP(resp, req)
	require.Equal(t, http.StatusCreated, resp.Result().StatusCode)
	state, err = ctrl2.getNodeState()
	require.NoError(t, err)
	require.Len(t, state.AppInstallations, 3, state)
	appID := ""
	for id := range state.AppInstallations {
		if !strings.HasPrefix(id, "app") {
			appID = id
			break
		}
	}

	// Uninstall app unit test
	handler = middleware.Middleware(http.HandlerFunc(ctrlServer.UninstallApp))
	b, err = json.Marshal(&UninstallAppRequest{
		AppID: appID,
	})
	require.NoError(t, err)
	req = httptest.NewRequest(
		http.MethodPost,
		`/uninstall-app/users/{user_id}`, // fake path for tests
		bytes.NewReader(b),
	)
	req.SetPathValue("user_id", "user1")

	resp = httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Result().StatusCode)
	state, err = ctrl2.getNodeState()
	require.NoError(t, err)
	require.Len(t, state.AppInstallations, 2)
}
