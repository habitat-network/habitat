package hdbms

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/rs/zerolog/log"
)

// TODO: this should really be a struct that is NodeState or something similar with a Persist()
// function to write it. Then we can have many implementations of NodeState where Persist() can persist
// to a local file, raft, or somewhere else.

// A simple "database" backed by a JSON file
type fileDB struct {
	// Take a lock over the entire DB state so no one can modify concurrently as we don't handle that as of yet
	mu sync.Mutex // protects below

	path  string
	state *hdb.JSONState
}

// fileDB implements db client.
var _ hdb.Client = &fileDB{}

func (db *fileDB) Path() string {
	return db.path
}

// Helper function to check whether desired file exists already
func dbExists(path string) (bool, error) {
	// TODO we need a much cleaner way to do this. Maybe a db metadata file.
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// Helper function to write byte state to a file
func writeState(path string, bytes []byte) error {
	return os.WriteFile(path, bytes, 0600)
}

func NewHabitatDB(path string, initialTransitions []hdb.Transition) (hdb.Client, error) {
	// First ensure that no db has the same name
	exists, err := dbExists(path)
	if err != nil {
		return nil, err
	}

	var state *hdb.JSONState
	sch := &node.NodeSchema{}

	// If file exists already, initialize state to what is in the file
	if exists {
		bytes, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		state, err = hdb.NewJSONState(sch, bytes)
		if err != nil {
			return nil, err
		}
	} else { // Otherwise, create state according to initialTransitions and write it

		// Create empty schema according to the NodeSchema
		empty, err := sch.EmptyState()
		if err != nil {
			return nil, err
		}
		bytes, err := empty.Bytes()
		if err != nil {
			return nil, err
		}

		// Marshal empty schema into JSONState
		emptyState, err := hdb.NewJSONState(sch, bytes)
		if err != nil {
			return nil, err
		}

		// Transition empty state to initial state from initialTransactions
		state, _, err = proposeTransitions(emptyState, initialTransitions)
		if err != nil {
			return nil, err
		}

		// Write out the initial state
		if writeState(path, state.Bytes()) != nil {
			return nil, err
		}
	}

	db := &fileDB{
		mu:    sync.Mutex{},
		path:  path,
		state: state,
	}
	return db, nil
}

// Helper function to statelessly generate new state from old + transitions
// Returns the new state, wrappers for logging / debugging purposes, error if any
func proposeTransitions(old *hdb.JSONState, transitions []hdb.Transition) (*hdb.JSONState, []*hdb.TransitionWrapper, error) {
	// Make a temp copy of the state to operate on
	tmp, err := old.Copy()
	if err != nil {
		return nil, nil, err
	}

	wrappers := make([]*hdb.TransitionWrapper, 0)

	for _, t := range transitions {
		err = t.Validate(tmp.Bytes())
		if err != nil {
			return nil, nil, fmt.Errorf("transition validation failed: %s", err)
		}

		patch, err := t.Patch(tmp.Bytes())
		if err != nil {
			return nil, nil, err
		}

		err = tmp.ApplyPatch(patch)
		if err != nil {
			return nil, nil, err
		}

		wrapped, err := hdb.WrapTransition(t, patch, tmp.Bytes())
		if err != nil {
			return nil, nil, err
		}

		wrappers = append(wrappers, wrapped)
	}

	return tmp, wrappers, nil
}

func (db *fileDB) ProposeTransitions(transitions []hdb.Transition) (*hdb.JSONState, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	newState, wrapped, err := proposeTransitions(db.state, transitions)
	if err != nil {
		return nil, err
	}

	// Write to the database
	err = writeState(db.path, newState.Bytes())
	if err == nil {
		wrappedJSON, err := json.Marshal(wrapped)
		if err == nil {
			log.Info().Msgf("Wrote new state: %s", string(wrappedJSON))
		} else {
			log.Err(fmt.Errorf("error marshalling wrapped transitions: %w", err))
		}
		// Only update state internals if write succeeds
		// TODO: fix, this seems hacky
		db.state = newState
	}
	return newState, nil
}

// Get the state as bytes
func (db *fileDB) Bytes() []byte {
	return db.state.Bytes()
}
