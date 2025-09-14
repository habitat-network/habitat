package state

import (
	"encoding/json"
	"testing"

	"github.com/eagraf/habitat-new/internal/app"
	"github.com/eagraf/habitat-new/internal/node/reverse_proxy"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTransitions(oldState *NodeState, transitions []Transition) (*NodeState, error) {
	var err error
	if oldState == nil {
		oldState, err = Schema.EmptyState()
		if err != nil {
			return nil, err
		}

	}
	oldBytes, err := oldState.Bytes()
	if err != nil {
		return nil, err
	}

	newStateBytes, _, err := ProposeTransitions(oldBytes, transitions)
	if err != nil {
		return nil, err
	}

	var state NodeState
	err = json.Unmarshal(newStateBytes, &state)
	if err != nil {
		return nil, err
	}

	return &state, nil
}

func TestFrontendDevMode(t *testing.T) {
	state, err := NewStateForLatestVersion()
	require.NoError(t, err)
	state.SetRootUserCert("fake_user_cert")
	newState, err := testTransitions(state, []Transition{
		&addReverseProxyRuleTransition{
			Rule: &reverse_proxy.Rule{
				Type:    reverse_proxy.ProxyRuleRedirect,
				ID:      "default-rule-frontend",
				Matcher: "", // Root matcher
			},
		},
	})
	require.Nil(t, err)

	frontendRule, ok := (newState.ReverseProxyRules)["default-rule-frontend"]
	require.Equal(t, true, ok)
	require.Equal(t, reverse_proxy.ProxyRuleRedirect, frontendRule.Type)
}

// use this if you expect the transitions to cause an error
func testTransitionsOnCopy(oldState *NodeState, transitions []Transition) (*NodeState, error) {
	marshaled, err := json.Marshal(oldState)
	if err != nil {
		return nil, err
	}

	var copy NodeState
	err = json.Unmarshal(marshaled, &copy)
	if err != nil {
		return nil, err
	}

	return testTransitions(&copy, transitions)
}

func TestNodeInitialization(t *testing.T) {
	transitions := []Transition{
		&initalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: CurrentVersion,
			},
		},
	}

	state, err := testTransitions(nil, transitions)
	assert.Nil(t, err)
	assert.NotNil(t, state)
	assert.Equal(t, "abc", state.NodeID)
	assert.Equal(t, "123", state.Certificate)
	assert.Equal(t, "New Node", state.Name)
	assert.Equal(t, 0, len(state.Users))
	assert.Equal(t, 0, len(state.Processes))

	_, err = testTransitions(nil, []Transition{
		&initalizationTransition{
			InitState: nil,
		},
	})
	assert.NotNil(t, err)
}

func TestAddingUsers(t *testing.T) {
	transitions := []Transition{
		&initalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: CurrentVersion,
			},
		},
		CreateAddUserTransition("eagraf", "fake-did"),
	}

	newState, err := testTransitions(nil, transitions)
	assert.Nil(t, err)
	assert.NotNil(t, newState)
	assert.Equal(t, "abc", newState.NodeID)
	assert.Equal(t, "123", newState.Certificate)
	assert.Equal(t, "New Node", newState.Name)
	assert.Equal(t, 1, len(newState.Users))

	testSecondUserConflictOnUsername := []Transition{
		CreateAddUserTransition("eagraf", "fake-did"),
	}

	_, err = testTransitionsOnCopy(newState, testSecondUserConflictOnUsername)
	assert.NotNil(t, err)
}
func TestAppLifecycle(t *testing.T) {
	createTransition, _ := CreateStartInstallationTransition(
		"123",
		&app.Package{
			Driver:             app.DriverTypeDocker,
			RegistryURLBase:    "https://registry.com",
			RegistryPackageID:  "app_name1",
			RegistryPackageTag: "v1",
			DriverConfig:       map[string]interface{}{},
		},
		"1",
		"app_name1",
		[]*reverse_proxy.Rule{},
	)
	transitions := []Transition{
		&initalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: LatestVersion,
				Users: map[string]*User{
					"123": {
						ID:       "123",
						Username: "eagraf",
					},
				},
			},
		},
		createTransition,
	}

	newState, err := testTransitions(nil, transitions)
	require.Nil(t, err)
	require.NotNil(t, newState)
	assert.Equal(t, "abc", newState.NodeID)
	assert.Equal(t, "123", newState.Certificate)
	assert.Equal(t, "New Node", newState.Name)
	assert.Equal(t, 1, len(newState.Users))
	assert.Equal(t, 1, len(newState.AppInstallations))

	apps, err := newState.GetAppsForUser("123")
	assert.Nil(t, err)

	appInstall := apps[0]
	_, ok := newState.AppInstallations[appInstall.ID]
	assert.Equal(t, ok, true)
	assert.NotEmpty(t, appInstall.ID)
	assert.Equal(t, "app_name1", appInstall.Name)
	assert.Equal(t, app.LifecycleStateInstalling, appInstall.State)

	transition, _ := CreateStartInstallationTransition(
		"123",
		&app.Package{
			Driver:             app.DriverTypeDocker,
			RegistryURLBase:    "https://registry.com",
			RegistryPackageID:  "app_name1",
			RegistryPackageTag: "v1",
			DriverConfig:       map[string]interface{}{},
		},
		"1",
		"app_name1",
		[]*reverse_proxy.Rule{},
	)
	testSecondAppConflict := []Transition{
		transition,
	}

	_, err = testTransitionsOnCopy(newState, testSecondAppConflict)
	assert.NotNil(t, err)

	testInstallationCompleted := []Transition{
		&finishInstallationTransition{
			AppID: appInstall.ID,
		},
	}

	newState, err = testTransitionsOnCopy(newState, testInstallationCompleted)
	assert.Nil(t, err)
	assert.Equal(t, app.LifecycleStateInstalled, newState.AppInstallations[appInstall.ID].State)

	testUserDoesntExist := []Transition{
		&startInstallationTransition{
			appInstallation: &app.Installation{
				UserID:  "456",
				Name:    "app_name1",
				Version: "1",
				Package: &app.Package{
					Driver:             app.DriverTypeDocker,
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
				},
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testUserDoesntExist)
	assert.NotNil(t, err)

	trns, _ := CreateStartInstallationTransition(
		"123",
		&app.Package{
			Driver:             app.DriverTypeDocker,
			RegistryURLBase:    "https://registry.com",
			RegistryPackageID:  "app_name1",
			RegistryPackageTag: "v2",
			DriverConfig:       map[string]interface{}{},
		},
		"2",
		"app_name1",
		[]*reverse_proxy.Rule{},
	)
	testDifferentVersion := []Transition{
		trns,
	}
	_, err = testTransitionsOnCopy(newState, testDifferentVersion)
	assert.NotNil(t, err)

	testFinishOnAppThatsNotInstalling := []Transition{
		&finishInstallationTransition{
			AppID: "app_name1", // already installed
		},
	}

	_, err = testTransitionsOnCopy(newState, testFinishOnAppThatsNotInstalling)
	assert.NotNil(t, err)
}

func TestAppInstallReverseProxyRules(t *testing.T) {
	proxyRules := make(map[string]*reverse_proxy.Rule)
	transitions := []Transition{
		&initalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: LatestVersion,
				Users: map[string]*User{
					"123": {
						Username: "eagraf",
					},
				},
				ReverseProxyRules: proxyRules,
			},
		},
		&startInstallationTransition{
			appInstallation: &app.Installation{
				UserID:  "123",
				Name:    "app_name1",
				Version: "1",
				State:   app.LifecycleStateInstalled,
				Package: &app.Package{
					Driver:             app.DriverTypeDocker,
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
					DriverConfig:       map[string]interface{}{},
				},
			},
			NewProxyRules: []*reverse_proxy.Rule{
				{
					AppID:   "app1",
					Matcher: "/path",
					Target:  "http://localhost:8080",
					Type:    "redirect",
				},
			},
		},
	}

	newState, err := testTransitions(nil, transitions)
	require.Nil(t, err)
	require.NotNil(t, newState)
}

func TestProcesses(t *testing.T) {
	init := []Transition{
		&initalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: LatestVersion,
				Users: map[string]*User{
					"123": {
						ID:       "123",
						Username: "eagraf",
					},
				},
				AppInstallations: map[string]*app.Installation{
					"App1": {
						ID:      "App1",
						Name:    "app_name1",
						UserID:  "123",
						Version: "1",
						State:   app.LifecycleStateInstalled,
						Package: &app.Package{
							Driver:             app.DriverTypeDocker,
							RegistryURLBase:    "https://registry.com",
							RegistryPackageID:  "app_name1",
							RegistryPackageTag: "v1",
							DriverConfig:       map[string]interface{}{},
						},
					},
				},
			},
		},
	}
	oldState, err := testTransitions(nil, init)
	require.NoError(t, err)

	startTransition, id, err := CreateProcessStartTransition("App1", oldState)
	require.NoError(t, err)

	newState, err := testTransitions(oldState, []Transition{startTransition})
	require.Nil(t, err)
	require.NotNil(t, newState)
	assert.Equal(t, "abc", newState.NodeID)
	assert.Equal(t, "123", newState.Certificate)
	assert.Equal(t, "New Node", newState.Name)
	assert.Equal(t, 1, len(newState.Users))
	assert.Equal(t, 1, len(newState.AppInstallations))
	assert.Equal(t, "app_name1", newState.AppInstallations["App1"].Name)
	assert.Equal(t, 1, len(newState.Processes))

	procs, err := newState.GetProcessesForUser("123")
	assert.Nil(t, err)
	assert.Equal(t, 1, len(procs))

	proc := procs[0]
	assert.Equal(t, proc.ID, id)
	assert.Equal(t, "App1", proc.AppID)
	assert.Equal(t, "123", proc.UserID)

	testProcessRunningNoMatchingID := []Transition{
		&processStartTransition{
			Process: &process.Process{
				AppID: proc.AppID,
			},
		},
	}
	_, err = testTransitionsOnCopy(newState, testProcessRunningNoMatchingID)
	assert.NotNil(t, err)

	testAppIDConflict := []Transition{
		&processStartTransition{
			Process: &process.Process{
				AppID: "App1",
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testAppIDConflict)
	assert.NotNil(t, err)

	// Test stopping the app
	newState, err = testTransitionsOnCopy(newState, []Transition{
		&processStopTransition{
			ProcessID: proc.ID,
		},
	})

	assert.Nil(t, err)
	assert.Equal(t, 0, len(newState.Processes))

	testUserDoesntExist := []Transition{
		&processStartTransition{
			Process: &process.Process{
				ID:     "proc2",
				AppID:  "App1",
				UserID: "456",
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testUserDoesntExist)
	assert.NotNil(t, err)

	testAppDoesntExist := []Transition{
		&processStartTransition{
			Process: &process.Process{
				ID:     "proc3",
				AppID:  "App2",
				UserID: "123",
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testAppDoesntExist)
	assert.NotNil(t, err)

	testProcessStopNoMatchingID := []Transition{
		&processStopTransition{
			ProcessID: "proc500",
		},
	}
	_, err = testTransitionsOnCopy(newState, testProcessStopNoMatchingID)
	assert.NotNil(t, err)
}

func TestMigrationsTransition(t *testing.T) {
	transitions := []Transition{
		&initalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: "v0.0.1",
				Users: map[string]*User{
					"123": {
						ID:       "123",
						Username: "eagraf",
					},
				},
			},
		},
		&migrationTransition{
			TargetVersion: "v0.0.2",
		},
	}

	newState, err := testTransitions(nil, transitions)
	assert.Nil(t, err)
	assert.Equal(t, "v0.0.2", newState.SchemaVersion)
	assert.Equal(t, "test", newState.TestField)

	testRemovingField := []Transition{
		&migrationTransition{
			TargetVersion: "v0.0.3",
		},
	}

	newState, err = testTransitionsOnCopy(newState, testRemovingField)
	assert.Nil(t, err)
	assert.Equal(t, "v0.0.3", newState.SchemaVersion)
	assert.Equal(t, "", newState.TestField)

	// Test migrating down
	testDown := []Transition{
		&migrationTransition{
			TargetVersion: "v0.0.1",
		},
	}
	newState, err = testTransitionsOnCopy(newState, testDown)
	assert.Nil(t, err)
	assert.Equal(t, "v0.0.1", newState.SchemaVersion)
	assert.Equal(t, "", newState.TestField)
}
