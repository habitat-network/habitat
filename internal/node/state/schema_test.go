package state

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/eagraf/habitat-new/internal/app"
	"github.com/eagraf/habitat-new/internal/node/reverse_proxy"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/qri-io/jsonschema"
	"github.com/stretchr/testify/assert"
)

func TestSchemaParsing(t *testing.T) {
	rs := &jsonschema.Schema{}
	err := json.Unmarshal([]byte(nodeSchemaRaw), rs)
	assert.Nil(t, err)

	// Test that an empty state from InitState() is valid
	initState, err := Schema.EmptyState()
	assert.Nil(t, err)
	assert.NotNil(t, Schema.Type())
	assert.Equal(t, "node", Schema.Name())

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

func TestGetAppByID(t *testing.T) {
	state := &NodeState{
		AppInstallations: map[string]*app.Installation{
			"app1": {
				ID: "app1",
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

	state := &NodeState{
		AppInstallations: map[string]*app.Installation{
			"app1": {
				ID: "app1",
			},
		},
		Processes: map[process.ID]*process.Process{
			"process1": {
				ID:    "process1",
				AppID: "app1",
			},
			"process2": {
				ID:    "process2",
				AppID: "non-existant-this-shouldn'thappen",
			},
		},
		ReverseProxyRules: map[string]*reverse_proxy.Rule{
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
}
