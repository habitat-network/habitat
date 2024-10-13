package controller

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/config"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func testTransitions(oldState *node.State, transitions []hdb.Transition) (*node.State, error) {
	var prevState *hdb.JSONState
	schema := &node.NodeSchema{}
	if oldState == nil {
		emptyState, err := schema.EmptyState()
		if err != nil {
			return nil, err
		}
		ojs, err := hdb.StateToJSONState(emptyState)
		if err != nil {
			return nil, err
		}
		prevState = ojs
	} else {
		ojs, err := hdb.StateToJSONState(oldState)
		if err != nil {
			return nil, err
		}

		prevState = ojs
	}
	// Continuously update prevState with each transition
	for _, t := range transitions {
		if t.Type() == "" {
			return nil, fmt.Errorf("transition type is empty")
		}

		err := t.Enrich(prevState.Bytes())
		if err != nil {
			return nil, fmt.Errorf("transition enrichment failed: %s", err)
		}

		err = t.Validate(prevState.Bytes())
		if err != nil {
			return nil, fmt.Errorf("transition validation failed: %s", err)
		}

		patch, err := t.Patch(prevState.Bytes())
		if err != nil {
			return nil, err
		}

		newStateBytes, err := prevState.ValidatePatch(patch)
		if err != nil {
			return nil, err
		}

		newState, err := hdb.NewJSONState(schema, newStateBytes)
		if err != nil {
			return nil, err
		}

		prevState = newState
	}

	var state node.State
	stateBytes := prevState.Bytes()

	err := json.Unmarshal(stateBytes, &state)
	if err != nil {
		return nil, err
	}

	return &state, nil
}

func TestInitTransitions(t *testing.T) {
	config, err := config.NewTestNodeConfig(nil)
	require.Nil(t, err)

	transitions, err := initTranstitions(config)
	require.Nil(t, err)

	require.Equal(t, 4, len(transitions))
}

func TestFrontendDevMode(t *testing.T) {
	v := viper.New()
	v.Set("frontend_dev", true)
	config, err := config.NewTestNodeConfig(v)
	require.Nil(t, err)

	config.RootUserCert = &x509.Certificate{}

	transitions, err := initTranstitions(config)
	require.Nil(t, err)

	require.Equal(t, 4, len(transitions))

	newState, err := testTransitions(nil, transitions)
	require.Nil(t, err)

	frontendRule, ok := (*newState.ReverseProxyRules)["default-rule-frontend"]
	require.Equal(t, true, ok)
	require.Equal(t, node.ProxyRuleRedirect, frontendRule.Type)
}
