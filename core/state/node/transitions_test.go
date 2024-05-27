package node

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/stretchr/testify/assert"
)

func testTransitions(oldState *NodeState, transitions []hdb.Transition) (*NodeState, error) {
	var oldJSONState *hdb.JSONState
	schema := &NodeSchema{}
	if oldState == nil {
		emptyState, err := schema.EmptyState()
		if err != nil {
			return nil, err
		}
		ojs, err := hdb.StateToJSONState(emptyState)
		if err != nil {
			return nil, err
		}
		oldJSONState = ojs
	} else {
		ojs, err := hdb.StateToJSONState(oldState)
		if err != nil {
			return nil, err
		}

		oldJSONState = ojs
	}
	for _, t := range transitions {
		if t.Type() == "" {
			return nil, fmt.Errorf("transition type is empty")
		}

		err := t.Validate(oldJSONState.Bytes())
		if err != nil {
			return nil, fmt.Errorf("transition validation failed: %s", err)
		}

		patch, err := t.Patch(oldJSONState.Bytes())
		if err != nil {
			return nil, err
		}

		newStateBytes, err := oldJSONState.ValidatePatch(patch)
		if err != nil {
			return nil, err
		}

		newState, err := hdb.NewJSONState(schema, newStateBytes)
		if err != nil {
			return nil, err
		}

		oldJSONState = newState
	}

	var state NodeState
	stateBytes := oldJSONState.Bytes()

	err := json.Unmarshal(stateBytes, &state)
	if err != nil {
		return nil, err
	}

	return &state, nil
}

// use this if you expect the transitions to cause an error
func testTransitionsOnCopy(oldState *NodeState, transitions []hdb.Transition) (*NodeState, error) {
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
	transitions := []hdb.Transition{
		&InitalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: "v0.0.1",
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

	_, err = testTransitions(nil, []hdb.Transition{
		&InitalizationTransition{
			InitState: nil,
		},
	})
	assert.NotNil(t, err)
}

func TestAddingUsers(t *testing.T) {
	transitions := []hdb.Transition{
		&InitalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: "v0.0.1",
			},
		},
		&AddUserTransition{
			UserID:      "123",
			Username:    "eagraf",
			Certificate: "placeholder",
		},
	}

	newState, err := testTransitions(nil, transitions)
	assert.Nil(t, err)
	assert.NotNil(t, newState)
	assert.Equal(t, "abc", newState.NodeID)
	assert.Equal(t, "123", newState.Certificate)
	assert.Equal(t, "New Node", newState.Name)
	assert.Equal(t, 1, len(newState.Users))

	testSecondUserConflictOnUsername := []hdb.Transition{
		&AddUserTransition{
			UserID:      "456",
			Username:    "eagraf",
			Certificate: "placeholder",
		},
	}

	_, err = testTransitionsOnCopy(newState, testSecondUserConflictOnUsername)
	assert.NotNil(t, err)

	testSecondUserConflictOnUserID := []hdb.Transition{
		&AddUserTransition{
			UserID:      "123",
			Username:    "eagraf2",
			Certificate: "placeholder",
		},
	}

	_, err = testTransitionsOnCopy(newState, testSecondUserConflictOnUserID)
	assert.NotNil(t, err)
}

func TestAppLifecycle(t *testing.T) {
	transitions := []hdb.Transition{
		&InitalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: LatestVersion,
				Users:         make(map[string]*User, 0),
			},
		},
		&AddUserTransition{
			UserID:      "123",
			Username:    "eagraf",
			Certificate: "placeholder",
		},
		&StartInstallationTransition{
			UserID: "123",
			AppInstallation: &AppInstallation{
				UserID:  "123",
				Name:    "app_name1",
				Version: "1",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
					DriverConfig:       map[string]interface{}{},
				},
			},
			NewProxyRules: []*ReverseProxyRule{},
		},
	}

	newState, err := testTransitions(nil, transitions)
	assert.Nil(t, err)
	assert.NotNil(t, newState)
	assert.Equal(t, "abc", newState.NodeID)
	assert.Equal(t, "123", newState.Certificate)
	assert.Equal(t, "New Node", newState.Name)
	assert.Equal(t, 1, len(newState.Users))
	assert.Equal(t, 1, len(newState.AppInstallations))

	apps, err := newState.GetAppsForUser("123")
	assert.Nil(t, err)

	app := apps[0]
	_, ok := newState.AppInstallations[app.ID]
	assert.Equal(t, ok, true)
	assert.NotEmpty(t, app.ID)
	assert.Equal(t, "app_name1", app.Name)
	assert.Equal(t, "installing", app.State)

	testSecondAppConflict := []hdb.Transition{
		&StartInstallationTransition{
			UserID: "123",
			AppInstallation: &AppInstallation{
				Version: "1",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
				},
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testSecondAppConflict)
	assert.NotNil(t, err)

	testUserDoesntExistOnFinish := []hdb.Transition{
		&FinishInstallationTransition{
			AppID:           app.ID,
			UserID:          "456",
			RegistryURLBase: "https://registry.com",
			RegistryAppID:   "app_name1",
		},
	}
	_, err = testTransitionsOnCopy(newState, testUserDoesntExistOnFinish)
	assert.NotNil(t, err)

	testInstallationCompleted := []hdb.Transition{
		&FinishInstallationTransition{
			UserID:          "123",
			AppID:           app.ID,
			RegistryURLBase: "https://registry.com",
			RegistryAppID:   "app_name1",
		},
	}

	newState, err = testTransitionsOnCopy(newState, testInstallationCompleted)
	assert.Nil(t, err)
	assert.Equal(t, "installed", newState.AppInstallations[app.ID].State)

	testUserDoesntExist := []hdb.Transition{
		&StartInstallationTransition{
			UserID: "456",
			AppInstallation: &AppInstallation{
				Name:    "app_name1",
				Version: "1",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
				},
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testUserDoesntExist)
	assert.NotNil(t, err)

	testDifferentVersion := []hdb.Transition{
		&StartInstallationTransition{
			UserID: "123",
			AppInstallation: &AppInstallation{
				ID:      "app1",
				Name:    "app_name1",
				Version: "2",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v2",
				},
			},
		},
	}
	_, err = testTransitionsOnCopy(newState, testDifferentVersion)
	assert.NotNil(t, err)

	testFinishOnAppThatsNotInstalling := []hdb.Transition{
		&FinishInstallationTransition{
			UserID:          "123",
			RegistryURLBase: "https://registry.com",
			RegistryAppID:   "app_name1",
		},
	}

	_, err = testTransitionsOnCopy(newState, testFinishOnAppThatsNotInstalling)
	assert.NotNil(t, err)
}

func TestAppInstallReverseProxyRules(t *testing.T) {
	proxyRules := make(map[string]*ReverseProxyRule)
	transitions := []hdb.Transition{
		&InitalizationTransition{
			InitState: &NodeState{
				NodeID:            "abc",
				Certificate:       "123",
				Name:              "New Node",
				SchemaVersion:     LatestVersion,
				Users:             make(map[string]*User, 0),
				ReverseProxyRules: &proxyRules,
			},
		},
		&AddUserTransition{
			UserID:      "123",
			Username:    "eagraf",
			Certificate: "placeholder",
		},
		&StartInstallationTransition{
			UserID: "123",
			AppInstallation: &AppInstallation{
				UserID:  "123",
				Name:    "app_name1",
				Version: "1",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
					DriverConfig:       map[string]interface{}{},
				},
			},
			NewProxyRules: []*ReverseProxyRule{
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
	assert.Nil(t, err)
	assert.NotNil(t, newState)
}

func TestProcesses(t *testing.T) {

	transitions := []hdb.Transition{
		&InitalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: LatestVersion,
				Users: map[string]*User{
					"123": {
						ID:          "123",
						Username:    "eagraf",
						Certificate: "placeholder",
					},
				},
				AppInstallations: map[string]*AppInstallationState{
					"App1": {
						AppInstallation: &AppInstallation{
							ID:      "App1",
							Name:    "app_name1",
							Version: "1",
							Package: Package{
								Driver:             "docker",
								RegistryURLBase:    "https://registry.com",
								RegistryPackageID:  "app_name1",
								RegistryPackageTag: "v1",
								DriverConfig:       map[string]interface{}{},
							},
						},
						State: "installed",
					},
				},
			},
		},
		&ProcessStartTransition{
			Process: &Process{
				AppID:  "App1",
				UserID: "123",
				Driver: "docker",
			},
			App: &AppInstallation{
				ID:      "App1",
				Name:    "app_name1",
				Version: "1",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
				},
			},
		},
	}

	newState, err := testTransitions(nil, transitions)
	assert.Nil(t, err)
	assert.NotNil(t, newState)
	assert.Equal(t, "abc", newState.NodeID)
	assert.Equal(t, "123", newState.Certificate)
	assert.Equal(t, "New Node", newState.Name)
	assert.Equal(t, 1, len(newState.Users))
	assert.Equal(t, 1, len(newState.AppInstallations))
	assert.Equal(t, "app_name1", newState.AppInstallations["App1"].Name)
	assert.Equal(t, "installed", newState.AppInstallations["App1"].State)
	assert.Equal(t, 1, len(newState.Processes))

	procs, err := newState.GetProcessesForUser("123")
	assert.Nil(t, err)
	assert.Equal(t, 1, len(procs))

	proc := procs[0]
	assert.NotEmpty(t, proc.ID)
	assert.Equal(t, "starting", proc.State)
	assert.Equal(t, "App1", proc.AppID)
	assert.Equal(t, "123", proc.UserID)

	// The app moves to running state

	testProcessRunning := []hdb.Transition{
		&ProcessRunningTransition{
			ProcessID: proc.ID,
		},
	}

	newState, err = testTransitionsOnCopy(newState, testProcessRunning)
	assert.Nil(t, err)
	assert.Equal(t, "running", newState.Processes[proc.ID].State)

	testProcessRunningNoMatchingID := []hdb.Transition{
		&ProcessRunningTransition{
			ProcessID: "proc2",
		},
	}
	_, err = testTransitionsOnCopy(newState, testProcessRunningNoMatchingID)
	assert.NotNil(t, err)

	testProcessIDConflict := []hdb.Transition{
		&ProcessStartTransition{
			Process: &Process{
				ID:     "proc1",
				AppID:  "App1",
				UserID: "123",
			},
			App: &AppInstallation{
				ID:      "App1",
				Name:    "app_name1",
				Version: "1",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
				},
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testProcessIDConflict)
	assert.NotNil(t, err)

	testProcessAlreadyRunning := []hdb.Transition{
		&ProcessRunningTransition{
			ProcessID: "proc1",
		},
	}

	_, err = testTransitionsOnCopy(newState, testProcessAlreadyRunning)
	assert.NotNil(t, err)

	testAppIDConflict := []hdb.Transition{
		&ProcessStartTransition{
			Process: &Process{
				ID:     "proc2",
				AppID:  "App1",
				UserID: "123",
			},
			App: &AppInstallation{
				ID:      "App1",
				Name:    "app_name1",
				Version: "1",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
				},
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testAppIDConflict)
	assert.NotNil(t, err)

	// Test stopping the app
	newState, err = testTransitionsOnCopy(newState, []hdb.Transition{
		&ProcessStopTransition{
			ProcessID: proc.ID,
		},
	})

	assert.Nil(t, err)
	assert.Equal(t, 0, len(newState.Processes))

	testUserDoesntExist := []hdb.Transition{
		&ProcessStartTransition{
			Process: &Process{
				ID:     "proc2",
				AppID:  "App1",
				UserID: "456",
			},
			App: &AppInstallation{
				ID:      "App1",
				Name:    "app_name1",
				Version: "1",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
				},
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testUserDoesntExist)
	assert.NotNil(t, err)

	testAppDoesntExist := []hdb.Transition{
		&ProcessStartTransition{
			Process: &Process{
				ID:     "proc3",
				AppID:  "App2",
				UserID: "123",
			},
			App: &AppInstallation{
				ID:      "App2",
				Name:    "app_name1",
				Version: "1",
				Package: Package{
					Driver:             "docker",
					RegistryURLBase:    "https://registry.com",
					RegistryPackageID:  "app_name1",
					RegistryPackageTag: "v1",
				},
			},
		},
	}

	_, err = testTransitionsOnCopy(newState, testAppDoesntExist)
	assert.NotNil(t, err)

	testProcessStopNoMatchingID := []hdb.Transition{
		&ProcessStopTransition{
			ProcessID: "proc500",
		},
	}
	_, err = testTransitionsOnCopy(newState, testProcessStopNoMatchingID)
	assert.NotNil(t, err)
}

func TestMigrationsTransition(t *testing.T) {

	transitions := []hdb.Transition{
		&InitalizationTransition{
			InitState: &NodeState{
				NodeID:        "abc",
				Certificate:   "123",
				Name:          "New Node",
				SchemaVersion: "v0.0.1",
				Users: map[string]*User{
					"123": {
						ID:          "123",
						Username:    "eagraf",
						Certificate: "placeholder",
					},
				},
			},
		},
		&MigrationTransition{
			TargetVersion: "v0.0.2",
		},
	}

	newState, err := testTransitions(nil, transitions)
	assert.Nil(t, err)
	assert.Equal(t, "v0.0.2", newState.SchemaVersion)
	assert.Equal(t, "test", newState.TestField)

	testRemovingField := []hdb.Transition{
		&MigrationTransition{
			TargetVersion: "v0.0.3",
		},
	}

	newState, err = testTransitionsOnCopy(newState, testRemovingField)
	assert.Nil(t, err)
	assert.Equal(t, "v0.0.3", newState.SchemaVersion)
	assert.Equal(t, "", newState.TestField)

	// Test migrating down
	testDown := []hdb.Transition{
		&MigrationTransition{
			TargetVersion: "v0.0.1",
		},
	}
	newState, err = testTransitionsOnCopy(newState, testDown)
	assert.Nil(t, err)
	assert.Equal(t, "v0.0.1", newState.SchemaVersion)
	assert.Equal(t, "", newState.TestField)
}
