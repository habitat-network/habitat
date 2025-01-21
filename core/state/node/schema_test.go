package node

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/qri-io/jsonschema"
	"github.com/stretchr/testify/assert"
)

func TestSchemaParsing(t *testing.T) {
	rs := &jsonschema.Schema{}
	err := json.Unmarshal([]byte(nodeSchemaRaw), rs)
	assert.Nil(t, err)

	// Test that an empty state from InitState() is valid
	ns := &NodeSchema{}
	initState, err := ns.EmptyState()
	assert.Nil(t, err)
	assert.NotNil(t, ns.Type())
	assert.Equal(t, "node", ns.Name())

	marshaled, err := json.Marshal(initState)
	assert.Nil(t, err)
	keyErrs, err := rs.ValidateBytes(context.Background(), marshaled)
	assert.Nil(t, err)
	if len(keyErrs) != 0 {
		for _, e := range keyErrs {
			t.Log(e)
		}
		t.Error()
	}
}

func TestInitializationTransition(t *testing.T) {
	ns := &NodeSchema{}
	initState, err := ns.EmptyState()
	assert.Nil(t, err)
	initStateBytes, err := json.Marshal(initState)
	assert.Nil(t, err)

	// Test that the initialization transition works
	it, err := ns.InitializationTransition(initStateBytes)
	assert.Nil(t, err)

	// Test that the transition is valid
	err = it.Validate([]byte{})
	assert.Nil(t, err)

	// Test that the patch is valid
	_, err = it.Patch([]byte{})
	assert.Nil(t, err)
}

func TestGetAppByID(t *testing.T) {
	state := &State{
		AppInstallations: map[string]*AppInstallationState{
			"app1": {
				AppInstallation: &AppInstallation{
					ID: "app1",
				},
			},
		},
	}

	app, err := state.GetAppByID("app1")
	assert.Nil(t, err)
	assert.Equal(t, "app1", app.ID)

	_, err = state.GetAppByID("app2")
	assert.NotNil(t, err)
}

func TestGetReverseProxyRulesForProcess(t *testing.T) {

	state := &State{
		AppInstallations: map[string]*AppInstallationState{
			"app1": {
				AppInstallation: &AppInstallation{
					ID: "app1",
				},
			},
		},
		Processes: map[string]*Process{
			"process1": &Process{
				ID:    "process1",
				AppID: "app1",
			},
			"process2": &Process{
				ID:    "process2",
				AppID: "non-existant-this-shouldn'thappen",
			},
		},
		ReverseProxyRules: &map[string]*ReverseProxyRule{
			"rule1": {
				ID:    "rule1",
				AppID: "app1",
			},
		},
	}

	rules, err := state.GetReverseProxyRulesForProcess("process1")
	assert.Nil(t, err)
	assert.Equal(t, 1, len(rules))
	assert.Equal(t, "rule1", rules[0].ID)

	_, err = state.GetReverseProxyRulesForProcess("nonexistant")
	assert.NotNil(t, err)
}

func TestGetEmptyStateForVersion(t *testing.T) {
	_, err := GetEmptyStateForVersion("v1")
	assert.NotNil(t, err)

	state, err := GetEmptyStateForVersion("v0.0.1")
	assert.Nil(t, err)
	assert.NotNil(t, state)

	state, err = GetEmptyStateForVersion("v0.0.4")
	assert.Nil(t, err)
	assert.NotNil(t, state)
	assert.NotNil(t, state.ReverseProxyRules)
}
