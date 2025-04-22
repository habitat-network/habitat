package test_helpers

import (
	"encoding/json"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
)

type NodeStateUpdate struct {
	// Embed the metadata struct so this fully implements hdb.StateUpdate.
	*hdb.StateUpdateMetadata

	state             *node.State
	transitionWrapper *hdb.TransitionWrapper
}

func NewNodeStateUpdate(state *node.State, transitionWrapper *hdb.TransitionWrapper, metadata *hdb.StateUpdateMetadata) *NodeStateUpdate {
	return &NodeStateUpdate{
		state:               state,
		transitionWrapper:   transitionWrapper,
		StateUpdateMetadata: metadata,
	}
}

func (n *NodeStateUpdate) NewState() hdb.State {
	return n.state
}

func (n *NodeStateUpdate) Transition() []byte {
	return n.transitionWrapper.Transition
}

func (n *NodeStateUpdate) TransitionType() hdb.TransitionType {
	return n.transitionWrapper.Type
}

func (n *NodeStateUpdate) NodeState() *node.State {
	return n.state
}

func StateUpdateTestHelper(transition hdb.Transition, newState *node.State) (hdb.StateUpdate, error) {
	transBytes, err := json.Marshal(transition)
	if err != nil {
		return nil, err
	}

	return NewNodeStateUpdate(newState, &hdb.TransitionWrapper{
		Transition: transBytes,
		Type:       transition.Type(),
	}, hdb.NewStateUpdateMetadata(1000, node.SchemaName, "test_db")), nil

}
