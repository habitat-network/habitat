package schemas

import (
	"fmt"

	"github.com/eagraf/habitat-new/core/state/node"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/eagraf/habitat-new/internal/node/hdb/state"
	"github.com/eagraf/habitat-new/internal/node/pubsub"
)

func GetSchema(schemaType string) (hdb.Schema, error) {
	switch schemaType {
	case node.SchemaName:
		return &node.NodeSchema{}, nil
	default:
		return nil, fmt.Errorf("schema type %s not found", schemaType)
	}
}

// TODO this should account for schema version too
func StateMachineFactory(databaseID string, schemaType string, emptyState []byte, replicator state.Replicator, publisher pubsub.Publisher[hdb.StateUpdate]) (state.StateMachineController, error) {
	var schema hdb.Schema

	switch schemaType {
	case node.SchemaName:
		schema = &node.NodeSchema{}
	default:
		return nil, fmt.Errorf("schema type %s not found", schemaType)
	}

	if emptyState == nil {
		initStateStruct, err := schema.EmptyState()
		if err != nil {
			return nil, err
		}
		emptyStateBytes, err := initStateStruct.Bytes()
		if err != nil {
			return nil, err
		}
		emptyState = emptyStateBytes
	}

	stateMachineController, err := state.NewStateMachine(databaseID, schema, emptyState, replicator, publisher)
	if err != nil {
		return nil, err
	}
	return stateMachineController, nil
}
