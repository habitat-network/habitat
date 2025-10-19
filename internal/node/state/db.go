package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/rs/zerolog/log"
)

var ErrDatabaseAlreadyExists = errors.New("database already exists")

// TODO: this should really be a struct that is NodeState or something similar with a Persist()
// function to write it. Then we can have many implementations of NodeState where Persist() can persist
// to a local file, raft, or somewhere else.

// A simple "database" backed by a JSON file
type fileDB struct {
	// Take a lock over the entire DB state so no one can modify concurrently as we don't handle that as of yet
	mu    sync.RWMutex // protects below
	path  string
	state SerializedState
}

// fileDB implements db client.
var _ Client = &fileDB{}

func (db *fileDB) Path() string {
	return db.path
}

// Helper function to write byte state to a file
func writeState(path string, bytes []byte) error {
	return os.WriteFile(path, bytes, 0o600)
}

func NewHabitatDB(path string, initialTransitions []Transition) (Client, error) {
	// First ensure that no db has the same name
	_, err := os.Stat(path)

	exists := true
	if errors.Is(err, os.ErrNotExist) {
		exists = false
	} else if err != nil {
		return nil, err
	}

	var state SerializedState

	// If file exists already, initialize state to what is in the file
	if exists {
		state, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		err = Schema.ValidateState(state)
		if err != nil {
			return nil, err
		}
	} else {
		// Otherwise, create state according to initialTransitions and write it
		// Create empty schema according to the NodeSchema
		empty, err := Schema.EmptyState()
		if err != nil {
			return nil, err
		}

		bytes, err := empty.Bytes()
		if err != nil {
			return nil, err
		}

		// Transition empty state to initial state from initialTransactions
		state, _, err = ProposeTransitions(bytes, initialTransitions)
		if err != nil {
			return nil, err
		}

		err = Schema.ValidateState(state)
		if err != nil {
			return nil, err
		}

		// Write out the initial state
		if writeState(path, state) != nil {
			return nil, err
		}
	}

	db := &fileDB{
		mu:    sync.RWMutex{},
		path:  path,
		state: state,
	}
	return db, nil
}

func (db *fileDB) ProposeTransitions(transitions []Transition) (SerializedState, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	newState, wrapped, err := ProposeTransitions(db.state, transitions)
	if err != nil {
		return nil, err
	}

	// Write to the database
	err = writeState(db.path, newState)
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
func (db *fileDB) Bytes() (SerializedState, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.state, nil
}

// Get the state as *NodeState
func (db *fileDB) State() (*NodeState, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return FromBytes(db.state)
}
