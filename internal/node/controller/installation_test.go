package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eagraf/habitat-new/internal/app"

	"github.com/eagraf/habitat-new/internal/process"
	"github.com/stretchr/testify/require"
)

type mockPkgManager struct {
	installs map[*app.Package]struct{}
}

func (m *mockPkgManager) Driver() app.DriverType {
	return app.DriverTypeUnknown
}
func (m *mockPkgManager) IsInstalled(packageSpec *app.Package, version string) (bool, error) {
	_, ok := m.installs[packageSpec]
	return ok, nil
}
func (m *mockPkgManager) InstallPackage(packageSpec *app.Package, version string) error {
	m.installs[packageSpec] = struct{}{}
	return nil
}
func (m *mockPkgManager) UninstallPackage(packageSpec *app.Package, version string) error {
	delete(m.installs, packageSpec)
	return nil
}

func (m *mockPkgManager) RestoreFromState(context.Context, map[string]*app.Installation) error {
	return nil
}

func TestInstallAppController(t *testing.T) {
	mockDriver := newMockDriver(app.DriverTypeDocker)

	state := fakeState()
	db := testDB(state)

	ctrl2, err := NewController(context.Background(), process.NewProcessManager([]process.Driver{mockDriver}),
		map[app.DriverType]app.PackageManager{
			app.DriverTypeDocker: &mockPkgManager{
				installs: make(map[*app.Package]struct{}),
			},
		},
		db,
		nil)
	require.NoError(t, err)
	ctrlServer, err := NewCtrlServer(
		context.Background(),
		ctrl2,
		state,
	)
	require.NoError(t, err)

	pkg := &app.Package{
		DriverConfig:       make(map[string]interface{}),
		Driver:             app.DriverTypeDocker,
		RegistryURLBase:    "https://registry.com",
		RegistryPackageID:  "app_name1",
		RegistryPackageTag: "v1",
	}

	// Same thing but with the server
	handler := http.HandlerFunc(ctrlServer.InstallApp)
	resp := httptest.NewRecorder()
	b, err := json.Marshal(&InstallAppRequest{
		AppInstallation: &app.Installation{
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
	handler = http.HandlerFunc(ctrlServer.UninstallApp)
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
