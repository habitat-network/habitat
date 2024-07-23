package node

import "github.com/eagraf/habitat-new/internal/node/hdb"

type NodeStateUpdate struct {
	// Embed the metadata struct so this fully implements hdb.StateUpdate.
	*hdb.StateUpdateMetadata

	state             *State
	transitionWrapper *hdb.TransitionWrapper
}

func NewNodeStateUpdate(state *State, transitionWrapper *hdb.TransitionWrapper, metadata *hdb.StateUpdateMetadata) *NodeStateUpdate {
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

func (n *NodeStateUpdate) TransitionType() string {
	return n.transitionWrapper.Type
}

func (n *NodeStateUpdate) NodeState() *State {
	return n.state
}
