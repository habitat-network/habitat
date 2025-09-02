package hdb

import (
	"encoding/json"
	"reflect"
)

// Aliases for serialized types.
type SerializedState []byte

type Schema interface {
	Name() string
	EmptyState() (State, error)
	Type() reflect.Type
	Initialize(initState []byte) (Transition, error)
	ValidateState(state []byte) error
}

type TransitionWrapper struct {
	Type       TransitionType `json:"type"`
	Patch      []byte         `json:"patch"`      // The JSON patch generated from the transition struct
	Transition []byte         `json:"transition"` // JSON encoded transition struct
}

type TransitionType string

const (
	TransitionInitialize          TransitionType = "initialize"
	TransitionMigrationUp         TransitionType = "migration_up"
	TransitionAddUser             TransitionType = "add_user"
	TransitionStartInstallation   TransitionType = "start_installation"
	TransitionFinishInstallation  TransitionType = "finish_installation"
	TransitionStartUninstallation TransitionType = "start_uninstallation"
	TransitionStartProcess        TransitionType = "process_start"
	TransitionStopProcess         TransitionType = "process_stop"
	TransitionAddReverseProxyRule TransitionType = "add_reverse_proxy_rule"
)

type Transition interface {
	// Type returns a string constant identifying the type of state transition. Currently these are in snake case.
	Type() TransitionType

	// Patch takes the old state and returns a JSON patch to apply to the old state to get the new state.
	Patch(oldState SerializedState) (SerializedState, error)

	// Validate checks that the transition is valid given the old state.
	Validate(oldState SerializedState) error
}

func WrapTransition(t Transition, patch []byte, oldState SerializedState) (*TransitionWrapper, error) {

	transition, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}

	return &TransitionWrapper{
		Type:       t.Type(),
		Patch:      patch,
		Transition: transition,
	}, nil
}

type State interface {
	Schema() Schema
	Bytes() ([]byte, error)
	Validate() error
}

func StateToJSONState(state State) (*JSONState, error) {
	stateBytes, err := state.Bytes()
	if err != nil {
		return nil, err
	}
	jsonState, err := NewJSONState(state.Schema(), stateBytes)
	if err != nil {
		return nil, err
	}

	return jsonState, nil
}

// An hdb.Client can transition a CRDT JSONState to a new result and also get the current state via Bytes()
type Client interface {
	ProposeTransitions(transitions []Transition) (*JSONState, error)
	Bytes() []byte
}
