package state

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/qri-io/jsonschema"
)

const SchemaName = "node"
const CurrentVersion = "v0.0.8"
const LatestVersion = "v0.0.8"

// This paackage contains core structs for the node  These are intended to be embedable in other structs
// throughout the application. That way, it's easy to modify the core struct, while having
// the component specific structs to be decoupled. Fields in these structs should be immutable.

// TODO to make these truly immutable, only methods should be exported, all fields should be private.

//go:embed schema/schema.json
var nodeSchemaRaw string

type nodeSchema struct{}

var Schema = &nodeSchema{}

func (s *nodeSchema) Name() string {
	return SchemaName
}

func (s *nodeSchema) EmptyState() (*NodeState, error) {
	return GetEmptyStateForVersion(CurrentVersion)
}

func (s *nodeSchema) Type() reflect.Type {
	return reflect.TypeOf(&NodeState{})
}

func (s *nodeSchema) JSONSchemaForVersion(version string) (*jsonschema.Schema, error) {
	migrations, err := readSchemaMigrationFiles()
	if err != nil {
		return nil, err
	}

	schema, err := getSchemaVersion(migrations, version)
	if err != nil {
		return nil, err
	}
	rs := &jsonschema.Schema{}
	err = json.Unmarshal([]byte(schema), rs)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON schema: %s", err)
	}

	return rs, nil
}

func (s *nodeSchema) ValidateState(state []byte) error {
	var stateObj NodeState
	err := json.Unmarshal(state, &stateObj)
	if err != nil {
		return err
	}

	jsonSchema, err := s.JSONSchemaForVersion(stateObj.SchemaVersion)
	if err != nil {
		return err
	}

	keyErrs, err := jsonSchema.ValidateBytes(context.Background(), state)
	if err != nil {
		return err
	}
	if len(keyErrs) > 0 {
		return keyErrs[0]
	}
	return nil
}
