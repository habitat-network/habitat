package test_helpers

import (
	"encoding/json"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
)

func StateUpdateTestHelper(transition hdb.Transition, newState *node.State) (hdb.StateUpdate, error) {
	stateBytes, err := newState.Bytes()
	if err != nil {
		return nil, err
	}
	err = transition.Enrich(stateBytes)
	if err != nil {
		return nil, err

	}

	transBytes, err := json.Marshal(transition)
	if err != nil {
		return nil, err
	}

	return node.NewNodeStateUpdate(newState, &hdb.TransitionWrapper{
		Transition: transBytes,
		Type:       transition.Type(),
	}, hdb.NewStateUpdateMetadata(1000, node.SchemaName, "test_db")), nil

}
