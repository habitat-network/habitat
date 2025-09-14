package state

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/eagraf/habitat-new/internal/app"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/reverse_proxy"
	"github.com/eagraf/habitat-new/internal/process"
	"github.com/google/uuid"
)

// TODO structs defined here can embed the immutable structs, but also include mutable fields.
type NodeState struct {
	NodeID        string           `json:"node_id"`
	Name          string           `json:"name"`
	Certificate   string           `json:"certificate"` // TODO turn this into b64
	SchemaVersion string           `json:"schema_version"`
	TestField     string           `json:"test_field,omitempty"`
	Users         map[string]*User `json:"users"`
	// A set of running processes that a node can restore to on startup.
	Processes         map[process.ID]*process.Process `json:"processes"`
	AppInstallations  map[string]*app.Installation    `json:"app_installations"`
	ReverseProxyRules map[string]*reverse_proxy.Rule  `json:"reverse_proxy_rules"`
}

func NewStateForLatestVersion() (*NodeState, error) {
	initState, err := GetEmptyStateForVersion(LatestVersion)
	if err != nil {
		return nil, err
	}
	initState.NodeID = uuid.New().String()
	return initState, nil
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	DID      string `json:"atproto_did,omitempty"`
}

func (s *NodeState) Bytes() ([]byte, error) {
	return json.Marshal(s)
}

func (s *NodeState) String() string {
	bytes, err := s.Bytes()
	if err != nil {
		return "Error in s.Bytes()"
	}
	return string(bytes)
}

func (s *NodeState) GetAppByID(appID string) (*app.Installation, error) {
	app, ok := s.AppInstallations[appID]
	if !ok {
		return nil, fmt.Errorf("app with ID %s not found", appID)
	}
	return app, nil
}

func (s *NodeState) GetAppsForUser(userID string) ([]*app.Installation, error) {
	apps := make([]*app.Installation, 0)
	for _, app := range s.AppInstallations {
		if app.UserID == userID {
			apps = append(apps, app)
		}
	}
	return apps, nil
}

func (s *NodeState) GetProcessesForUser(userID string) ([]*process.Process, error) {
	procs := make([]*process.Process, 0)
	for _, proc := range s.Processes {
		if proc.UserID == userID {
			procs = append(procs, proc)
		}
	}
	return procs, nil
}

func (s *NodeState) GetReverseProxyRulesForProcess(processID process.ID) ([]*reverse_proxy.Rule, error) {
	process, ok := s.Processes[process.ID(processID)]
	if !ok {
		return nil, fmt.Errorf("process with ID %s not found", processID)
	}
	app, ok := s.AppInstallations[process.AppID]
	if !ok {
		return nil, fmt.Errorf("app with ID %s not found", process.AppID)
	}
	rules := make([]*reverse_proxy.Rule, 0)
	for _, rule := range s.ReverseProxyRules {
		if rule.AppID == app.ID {
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func (s *NodeState) SetRootUserCert(rootUserCert string) {
	// TODO this is basically a placeholder until we actually have a way of generating
	// the certificate for the node.
	s.Users[constants.RootUserID] = &User{
		ID:       constants.RootUserID,
		Username: constants.RootUsername,
	}
}

func (s *NodeState) Copy() (*NodeState, error) {
	marshaled, err := s.Bytes()
	if err != nil {
		return nil, err
	}
	var copy NodeState
	err = json.Unmarshal(marshaled, &copy)
	if err != nil {
		return nil, err
	}
	return &copy, nil
}

func (s *NodeState) Validate() error {
	schemaVersion := s.SchemaVersion

	jsonSchema, err := Schema.JSONSchemaForVersion(schemaVersion)
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

func FromBytes(bytes []byte) (*NodeState, error) {
	var state NodeState
	err := json.Unmarshal(bytes, &state)
	return &state, err
}
