package state

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/eagraf/habitat-new/internal/pubsub"

	"github.com/rs/zerolog/log"
)

type Replicator interface {
	Dispatch([]byte) (*hdb.JSONState, error)
	UpdateChannel() <-chan hdb.StateUpdate
	IsLeader() bool
	GetLastCommandIndex() (uint64, error)
}

type StateMachineController interface {
	StartListening(context.Context)
	StopListening()
	DatabaseID() string
	Bytes() []byte
	ProposeTransitions(transitions []hdb.Transition) (*hdb.JSONState, error)
}

type StateMachine struct {
	restartIndex uint64
	databaseID   string
	jsonState    *hdb.JSONState // this JSONState is maintained in addition to
	publisher    pubsub.Publisher[hdb.StateUpdate]
	replicator   Replicator
	updateChan   <-chan hdb.StateUpdate
	doneChan     chan bool

	schema hdb.Schema
}

func NewStateMachine(databaseID string, schema hdb.Schema, initRawState []byte, replicator Replicator, publisher pubsub.Publisher[hdb.StateUpdate]) (StateMachineController, error) {
	jsonState, err := hdb.NewJSONState(schema, initRawState)
	if err != nil {
		return nil, err
	}

	restartIndex, err := replicator.GetLastCommandIndex()
	if err != nil {
		return nil, err
	}
	return &StateMachine{
		restartIndex: restartIndex,
		databaseID:   databaseID,
		jsonState:    jsonState,
		updateChan:   replicator.UpdateChannel(),
		replicator:   replicator,
		doneChan:     make(chan bool),
		publisher:    publisher,
		schema:       schema,
	}, nil
}

func (sm *StateMachine) StartListening(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		// TODO this bit of code should be well tested
		case stateUpdate := <-sm.updateChan:

			// Only publish state updates if this node is the leader node.
			if sm.replicator.IsLeader() {
				// Only apply state updates if the update index is greater than the restart index.
				// If the update index is equal to the restart index, then the state update is a
				// restore message which tells the subscribers to restore everything from the most up to date state.
				if sm.restartIndex > stateUpdate.Index() {
					continue
				}

				// execute state update
				stateBytes, err := stateUpdate.NewState().Bytes()
				if err != nil {
					log.Error().Err(err).Msgf("error getting new state bytes from state update chan")
				}
				jsonState, err := hdb.NewJSONState(sm.schema, stateBytes)
				if err != nil {
					log.Error().Err(err).Msgf("error getting new state from state update chan")
				}
				sm.jsonState = jsonState

				if sm.restartIndex == stateUpdate.Index() {
					log.Info().Msgf("Restoring node state")
					stateUpdate.SetRestore()
				}

				err = sm.publisher.PublishEvent(stateUpdate)
				if err != nil {
					log.Error().Err(err).Msgf("error publishing state update")
				}
			}
		case <-sm.doneChan:
			return
		}
	}
}

func (sm *StateMachine) StopListening() {
	sm.doneChan <- true
}

func (sm *StateMachine) DatabaseID() string {
	return sm.databaseID
}

func (sm *StateMachine) Bytes() []byte {
	return sm.jsonState.Bytes()
}

// ProposeTransitions takes a list of transitions and applies them to the current state
// The hypothetical new state is returned. Importantly, this does not block until the state
// is "officially updated".
func (sm *StateMachine) ProposeTransitions(transitions []hdb.Transition) (*hdb.JSONState, error) {
	jsonStateBranch, err := sm.jsonState.Copy()
	if err != nil {
		return nil, err
	}

	wrappers := make([]*hdb.TransitionWrapper, 0)

	for _, t := range transitions {
		err = t.Validate(jsonStateBranch.Bytes())
		if err != nil {
			return nil, fmt.Errorf("transition validation failed: %s", err)
		}

		patch, err := t.Patch(jsonStateBranch.Bytes())
		if err != nil {
			return nil, err
		}

		err = jsonStateBranch.ApplyPatch(patch)
		if err != nil {
			return nil, err
		}

		wrapped, err := hdb.WrapTransition(t, patch, jsonStateBranch.Bytes())
		if err != nil {
			return nil, err
		}

		wrappers = append(wrappers, wrapped)
	}

	transitionsJSON, err := json.Marshal(wrappers)
	if err != nil {
		return nil, err
	}
	log.Info().Msg(string(transitionsJSON))

	_, err = sm.replicator.Dispatch(transitionsJSON)
	if err != nil {
		return nil, err
	}

	return jsonStateBranch, nil
}
