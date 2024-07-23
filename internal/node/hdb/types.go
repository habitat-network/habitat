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
	InitializationTransition(initState []byte) (Transition, error)
	ValidateState(state []byte) error
}

type TransitionWrapper struct {
	Type       string `json:"type"`
	Patch      []byte `json:"patch"`      // The JSON patch generated from the transition struct
	Transition []byte `json:"transition"` // JSON encoded transition struct
}

type Transition interface {
	// Type returns a string constant identifying the type of state transition. Currently these are in snake case.
	Type() string

	// Patch takes the old state and returns a JSON patch to apply to the old state to get the new state.
	Patch(oldState SerializedState) (SerializedState, error)

	// Enrich adds important data to the transition that is not specified when submitted by the client.
	// For example, the id of a newly created object can be generated during the enrichment phase. The id is not
	// something we'd want the client to submit, but we want it to be included in the transition.
	Enrich(oldState SerializedState) error

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
