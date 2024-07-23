package state

import (
	"encoding/base64"
	"encoding/json"
	"io"

	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/hashicorp/raft"
	"github.com/rs/zerolog/log"
)

// RaftFSMAdapter makes our state machine struct play nice with the Raft library
// It treats all state as a JSON blob so that things are serializable, which makes it
// easy to decouple the state machine types from the Raft library
type RaftFSMAdapter struct {
	databaseID string
	jsonState  *hdb.JSONState
	updateChan chan hdb.StateUpdate
	schema     hdb.Schema
}

func NewRaftFSMAdapter(databaseID string, schema hdb.Schema, commState []byte) (*RaftFSMAdapter, error) {
	var jsonState *hdb.JSONState
	var err error
	if commState == nil {
		emptyState, err := schema.EmptyState()
		if err != nil {
			return nil, err
		}
		emptyStateBytes, err := emptyState.Bytes()
		if err != nil {
			return nil, err
		}
		jsonState, err = hdb.NewJSONState(schema, emptyStateBytes)
		if err != nil {
			return nil, err
		}
	} else {
		jsonState, err = hdb.NewJSONState(schema, commState)
		if err != nil {
			return nil, err
		}
	}

	return &RaftFSMAdapter{
		databaseID: databaseID,
		jsonState:  jsonState,
		updateChan: make(chan hdb.StateUpdate),
		schema:     schema,
	}, nil
}

func (sm *RaftFSMAdapter) JSONState() *hdb.JSONState {
	return sm.jsonState
}

func (sm *RaftFSMAdapter) UpdateChan() <-chan hdb.StateUpdate {
	return sm.updateChan
}

// Apply log is invoked once a log entry is committed.
// It returns a value which will be made available in the
// ApplyFuture returned by Raft.Apply method if that
// method was called on the same Raft node as the FSM.
func (sm *RaftFSMAdapter) Apply(entry *raft.Log) interface{} {
	buf, err := base64.StdEncoding.DecodeString(string(entry.Data))
	if err != nil {
		log.Error().Msgf("error decoding log entry data: %s", err)
	}

	var wrappers []*hdb.TransitionWrapper
	err = json.Unmarshal(buf, &wrappers)
	if err != nil {
		log.Error().Msgf("error unmarshaling transition wrapper: %s", err)
	}

	for _, w := range wrappers {
		err = sm.jsonState.ApplyPatch(w.Patch)
		if err != nil {
			log.Error().Msgf("error applying patch: %s", err)
		}

		metadata := hdb.NewStateUpdateMetadata(entry.Index, sm.schema.Name(), sm.databaseID)

		update, err := StateUpdateInternalFactory(sm.schema.Name(), sm.jsonState.Bytes(), w, metadata)
		if err != nil {
			log.Error().Msgf("error creating state update internal: %s", err)
		}

		sm.updateChan <- update
	}

	return sm.JSONState()
}

// Snapshot is used to support log compaction. This call should
// return an FSMSnapshot which can be used to save a point-in-time
// snapshot of the FSM. Apply and Snapshot are not called in multiple
// threads, but Apply will be called concurrently with Persist. This means
// the FSM should be implemented in a fashion that allows for concurrent
// updates while a snapshot is happening.
func (sm *RaftFSMAdapter) Snapshot() (raft.FSMSnapshot, error) {
	return &FSMSnapshot{
		state: sm.jsonState.Bytes(),
	}, nil
}

// Restore is used to restore an FSM from a snapshot. It is not called
// concurrently with any other command. The FSM must discard all previous
// state.
func (sm *RaftFSMAdapter) Restore(reader io.ReadCloser) error {
	buf, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	state, err := hdb.NewJSONState(sm.schema, buf)
	if err != nil {
		return err
	}

	sm.jsonState = state
	return nil
}

func (sm *RaftFSMAdapter) State() ([]byte, error) {
	return sm.jsonState.Bytes(), nil
}

type FSMSnapshot struct {
	state []byte
}

// Persist should dump all necessary state to the WriteCloser 'sink',
// and call sink.Close() when finished or call sink.Cancel() on error.
func (s *FSMSnapshot) Persist(sink raft.SnapshotSink) error {
	_, err := sink.Write(s.state)
	if err != nil {
		return err
	}
	return sink.Close()
}

// Release is invoked when we are finished with the snapshot.
func (s *FSMSnapshot) Release() {
}
