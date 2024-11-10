package node

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/qri-io/jsonschema"
)

const SchemaName = "node"
const CurrentVersion = "v0.0.6"
const LatestVersion = "v0.0.6"

//go:embed schema/schema.json
var nodeSchemaRaw string

// TODO structs defined here can embed the immutable structs, but also include mutable fields.

type State struct {
	NodeID            string                           `json:"node_id"`
	Name              string                           `json:"name"`
	Certificate       string                           `json:"certificate"` // TODO turn this into b64
	SchemaVersion     string                           `json:"schema_version"`
	TestField         string                           `json:"test_field,omitempty"`
	Users             map[string]*User                 `json:"users"`
	Processes         map[string]*ProcessState         `json:"processes"`
	AppInstallations  map[string]*AppInstallationState `json:"app_installations"`
	ReverseProxyRules *map[string]*ReverseProxyRule    `json:"reverse_proxy_rules,omitempty"`
}

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Certificate string `json:"certificate"` // TODO turn this into b64
	AtprotoDID  string `json:"atproto_did,omitempty"`
}

const AppStateInstalling = "installing"
const AppStateInstalled = "installed"
const AppStateUninstalled = "uninstalled"

type AppInstallationState struct {
	*AppInstallation `tstype:",extends,required"`
	State            string `json:"state"`
}

type ProcessState struct {
	*Process    `tstype:",extends,required"`
	State       string `json:"state"`
	ExtDriverID string `json:"ext_driver_id"`
}

func (s State) Schema() hdb.Schema {
	ns := &NodeSchema{}
	return ns
}

func (s State) Bytes() ([]byte, error) {
	return json.Marshal(s)
}

func (s State) GetAppByID(appID string) (*AppInstallationState, error) {
	app, ok := s.AppInstallations[appID]
	if !ok {
		return nil, fmt.Errorf("app with ID %s not found", appID)
	}
	return app, nil
}

func (s State) GetAppsForUser(userID string) ([]*AppInstallationState, error) {
	apps := make([]*AppInstallationState, 0)
	for _, app := range s.AppInstallations {
		if app.UserID == userID {
			apps = append(apps, app)
		}
	}
	return apps, nil
}

func (s State) GetProcessesForUser(userID string) ([]*ProcessState, error) {
	procs := make([]*ProcessState, 0)
	for _, proc := range s.Processes {
		if proc.UserID == userID {
			procs = append(procs, proc)
		}
	}
	return procs, nil
}

func (s State) GetReverseProxyRulesForProcess(processID string) ([]*ReverseProxyRule, error) {
	process, ok := s.Processes[processID]
	if !ok {
		return nil, fmt.Errorf("process with ID %s not found", processID)
	}
	app, ok := s.AppInstallations[process.AppID]
	if !ok {
		return nil, fmt.Errorf("app with ID %s not found", process.AppID)
	}
	rules := make([]*ReverseProxyRule, 0)
	for _, rule := range *s.ReverseProxyRules {
		if rule.AppID == app.ID {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func (s State) Copy() (*State, error) {
	marshaled, err := s.Bytes()
	if err != nil {
		return nil, err
	}
	var copy State
	err = json.Unmarshal(marshaled, &copy)
	if err != nil {
		return nil, err
	}
	return &copy, nil
}

func (s State) Validate() error {
	schemaVersion := s.SchemaVersion

	ns := &NodeSchema{}
	jsonSchema, err := ns.JSONSchemaForVersion(schemaVersion)
	if err != nil {
		return err
	}
	stateBytes, err := s.Bytes()
	if err != nil {
		return err
	}
	keyErrs, err := jsonSchema.ValidateBytes(context.Background(), stateBytes)
	if err != nil {
		return err
	}

	// Just return the first error.
	if len(keyErrs) > 0 {
		return keyErrs[0]
	}
	return nil
}

type NodeSchema struct{}

func (s *NodeSchema) Name() string {
	return SchemaName
}

func (s *NodeSchema) EmptyState() (hdb.State, error) {

	return GetEmptyStateForVersion(CurrentVersion)

}

func (s *NodeSchema) Type() reflect.Type {
	return reflect.TypeOf(&State{})
}

func (s *NodeSchema) InitializationTransition(initState []byte) (hdb.Transition, error) {
	var is *State
	err := json.Unmarshal(initState, &is)
	if err != nil {
		return nil, err
	}
	is.SchemaVersion = CurrentVersion
	return &InitalizationTransition{
		InitState: is,
	}, nil
}

func (s *NodeSchema) JSONSchemaForVersion(version string) (*jsonschema.Schema, error) {

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

func (s *NodeSchema) ValidateState(state []byte) error {
	var stateObj State
	err := json.Unmarshal(state, &stateObj)
	if err != nil {
		return err
	}

	version := stateObj.SchemaVersion

	jsonSchema, err := s.JSONSchemaForVersion(version)
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
