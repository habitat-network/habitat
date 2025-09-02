package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type initalizationTransition struct {
	InitState *NodeState `json:"init_state"`
}

func CreateInitializationTransition(state *NodeState) Transition {
	return &initalizationTransition{
		InitState: state,
	}
}

func (t *initalizationTransition) Type() TransitionType {
	return TransitionInitialize
}

func (t *initalizationTransition) Patch(oldState SerializedState) (SerializedState, error) {
	if t.InitState.Users == nil {
		t.InitState.Users = make(map[string]*User, 0)
	}

	if t.InitState.AppInstallations == nil {
		t.InitState.AppInstallations = make(map[string]*AppInstallation)
	}

	if t.InitState.Processes == nil {
		t.InitState.Processes = make(map[ProcessID]*Process)
	}

	if t.InitState.ReverseProxyRules == nil {
		t.InitState.ReverseProxyRules = make(map[string]*ReverseProxyRule)
	}

	marshaled, err := json.Marshal(t.InitState)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf(`[{
		"op": "add",
		"path": "",
		"value": %s
	}]`, marshaled)), nil
}

func (t *initalizationTransition) Validate(oldState SerializedState) error {
	if t.InitState == nil {
		return fmt.Errorf("init state cannot be nil")
	}
	return nil
}

type migrationTransition struct {
	TargetVersion string
}

func CreateMigrationTransition(targetVersion string) Transition {
	return &migrationTransition{
		TargetVersion: targetVersion,
	}
}

func (t *migrationTransition) Type() TransitionType {
	return TransitionMigrationUp
}

func (t *migrationTransition) Patch(oldState SerializedState) (SerializedState, error) {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return nil, err
	}

	patch, err := NodeDataMigrations.GetMigrationPatch(oldNode.SchemaVersion, t.TargetVersion, &oldNode)
	if err != nil {
		return nil, err
	}

	return json.Marshal(patch)
}

func (t *migrationTransition) Validate(oldState SerializedState) error {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	patch, err := NodeDataMigrations.GetMigrationPatch(oldNode.SchemaVersion, t.TargetVersion, &oldNode)
	if err != nil {
		return err
	}

	newState, err := applyPatchToState(patch, &oldNode)
	if err != nil {
		return err
	}

	err = newState.Validate()
	if err != nil {
		return err
	}

	return nil
}

type addUserTransition struct {
	User *User
}

func CreateAddUserTransition(username string, did string) Transition {
	return &addUserTransition{
		User: &User{
			Username: username,
			DID:      did,
			ID:       uuid.New().String(),
		},
	}
}

type AddUserTranstitionEnrichedData struct {
	User *User `json:"user"`
}

func (t *addUserTransition) Type() TransitionType {
	return TransitionAddUser
}

func (t *addUserTransition) Patch(oldState SerializedState) (SerializedState, error) {

	user, err := json.Marshal(t.User)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf(`[{
		"op": "add",
		"path": "/users/%s",
		"value": %s
	}]`, t.User.ID, user)), nil
}

func (t *addUserTransition) Validate(oldState SerializedState) error {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	_, ok := oldNode.Users[t.User.ID]
	if ok {
		return fmt.Errorf("user with id %s already exists", t.User.ID)
	}

	// Check for conflicting usernames
	for _, user := range oldNode.Users {
		if user.Username == t.User.Username {
			return fmt.Errorf("user with username %s already exists", t.User.Username)
		}
	}
	return nil
}

type startInstallationTransition struct {
	*AppInstallation
	NewProxyRules []*ReverseProxyRule `json:"new_proxy_rules"`
}

func (t *startInstallationTransition) Type() TransitionType {
	return TransitionStartInstallation
}

func (t *startInstallationTransition) Patch(oldState SerializedState) (SerializedState, error) {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return nil, err
	}

	marshalledApp, err := json.Marshal(t.AppInstallation)
	if err != nil {
		return nil, err
	}

	_, ok := oldNode.Users[t.UserID]
	if !ok {
		return nil, fmt.Errorf("user with id %s not found", t.UserID)
	}

	marshaledRules := make([]string, 0)
	for _, rule := range t.NewProxyRules {
		marshaled, err := json.Marshal(rule)
		if err != nil {
			return nil, err
		}
		op := fmt.Sprintf(`{
			"op": "add",
			"path": "/reverse_proxy_rules/%s",
			"value": %s
		}`, rule.ID, marshaled)
		marshaledRules = append(marshaledRules, op)
	}

	rules := ""
	if len(marshaledRules) != 0 {
		rules = "," + strings.Join(marshaledRules, ",")
	}

	return []byte(fmt.Sprintf(`[
		{
			"op": "add",
			"path": "/app_installations/%s",
			"value": %s
		}%s
	]`, t.AppInstallation.ID, string(marshalledApp), rules)), nil
}

func (t *startInstallationTransition) Validate(oldState SerializedState) error {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	_, ok := oldNode.Users[t.UserID]
	if !ok {
		return fmt.Errorf("user with id %s not found", t.UserID)
	}

	app, ok := oldNode.AppInstallations[t.AppInstallation.ID]
	if ok {
		if app.Version == t.Version {
			return fmt.Errorf("app %s version %s for user %s found", t.Name, t.Version, t.UserID)
		} else {
			// TODO eventually this will be part of an upgrade flow
			return fmt.Errorf("app %s for user %s found in state with different version %s", t.Name, t.UserID, app.Version)
		}
	}

	// Look for matching registry URL and package ID
	// TODO @eagraf - we need a way to update apps
	for _, app := range oldNode.AppInstallations {
		if app.RegistryURLBase == t.RegistryURLBase && app.RegistryPackageID == t.RegistryPackageID {
			return fmt.Errorf("app %s for user %s found in state with different version %s", app.Name, t.UserID, app.Version)
		}
	}

	if t.AppInstallation.DriverConfig == nil {
		return fmt.Errorf("driver config is required for starting an installation")
	}

	return nil
}

func CreateStartInstallationTransition(userID string, pkg *Package, version string, name string, proxyRules []*ReverseProxyRule) (Transition, string) {
	id := uuid.NewString()
	transition := &startInstallationTransition{
		AppInstallation: &AppInstallation{
			ID:      id,
			UserID:  userID,
			Name:    name,
			Version: version,
			State:   AppLifecycleStateInstalling,
			Package: pkg,
		},
		NewProxyRules: proxyRules,
	}
	return transition, id
}

type finishInstallationTransition struct {
	AppID string `json:"app_id"`
}

func CreateFinishInstallationTransition(appID string) Transition {
	return &finishInstallationTransition{
		AppID: appID,
	}
}

func (t *finishInstallationTransition) Type() TransitionType {
	return TransitionFinishInstallation
}
func (t *finishInstallationTransition) Patch(oldState SerializedState) (SerializedState, error) {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`[{
		"op": "replace",
		"path": "/app_installations/%s/state",
		"value": "%s"
	}]`, t.AppID, AppLifecycleStateInstalled)), nil
}

func (t *finishInstallationTransition) Validate(oldState SerializedState) error {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}
	app, ok := oldNode.AppInstallations[t.AppID]
	if !ok {
		return fmt.Errorf("app with id %s not found", t.AppID)
	}

	if app.State != "installing" {
		return fmt.Errorf("app %s is in state %s", app.Name, app.State)
	}
	return nil
}

// TODO handle uninstallation

type uninstallTransition struct {
	AppID string `json:"app_id"`
}

func CreateUninstallAppTransition(appID string) Transition {
	return &uninstallTransition{
		AppID: appID,
	}
}

func (t *uninstallTransition) Type() TransitionType {
	return TransitionStartUninstallation
}
func (t *uninstallTransition) Patch(oldState SerializedState) (SerializedState, error) {
	return []byte(fmt.Sprintf(`[{
		"op": "remove",
		"path": "/app_installations/%s"
	}]`, t.AppID)), nil
}

func (t *uninstallTransition) Validate(oldState SerializedState) error {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}
	_, ok := oldNode.AppInstallations[t.AppID]
	if !ok {
		return fmt.Errorf("app with id %s not found", t.AppID)
	}
	return nil
}

type processStartTransition struct {
	// Requested data
	Process *Process
}

func CreateProcessStartTransition(appID string, oldState *NodeState) (Transition, ProcessID, error) {
	app, err := oldState.GetAppByID(appID)
	if err != nil {
		return nil, "", err
	}

	id := NewProcessID(app.Driver)
	proc := &Process{
		ID:      id,
		UserID:  app.UserID,
		AppID:   app.ID,
		Created: time.Now().Format(time.RFC3339),
	}
	return &processStartTransition{
		Process: proc,
	}, id, nil
}

func (t *processStartTransition) Type() TransitionType {
	return TransitionStartProcess
}

func (t *processStartTransition) Patch(oldState SerializedState) (SerializedState, error) {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return nil, err
	}

	marshaled, err := json.Marshal(t.Process)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf(`[{
			"op": "add",
			"path": "/processes/%s",
			"value": %s
		}]`, t.Process.ID, marshaled)), nil
}

func (t *processStartTransition) Validate(oldState SerializedState) error {

	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	if t.Process == nil {
		return fmt.Errorf("process transition was not properly created")
	}

	// Make sure the app installation is in the installed state
	userID := t.Process.UserID
	app, err := oldNode.GetAppByID(t.Process.AppID)
	if err != nil {
		return err
	}
	if app.State != AppLifecycleStateInstalled {
		return fmt.Errorf("app with id %s is not in state %s", t.Process.AppID, AppLifecycleStateInstalled)
	}

	// Check user exists
	_, ok := oldNode.Users[t.Process.UserID]
	if !ok {
		return fmt.Errorf("user with id %s does not exist", userID)
	}
	if _, ok := oldNode.Processes[t.Process.ID]; ok {
		return fmt.Errorf("Process with id %s already exists", t.Process.ID)
	}

	for _, proc := range oldNode.Processes {
		// Make sure that no app with the same ID has a process
		if proc.AppID == t.Process.AppID {
			return fmt.Errorf("app with id %s already has a process; multiple processes per app not supported at this time", t.Process.AppID)
		}
	}

	return nil
}

type processStopTransition struct {
	ProcessID ProcessID `json:"process_id"`
}

func CreateProcessStopTransition(id ProcessID) Transition {
	return &processStopTransition{
		ProcessID: id,
	}
}

func (t *processStopTransition) Type() TransitionType {
	return TransitionStopProcess
}

var (
	ErrNoProcFound = errors.New("process with id not found")
)

func (t *processStopTransition) Patch(oldState SerializedState) (SerializedState, error) {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return nil, err
	}

	_, ok := oldNode.Processes[t.ProcessID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoProcFound, t.ProcessID)
	}

	return []byte(fmt.Sprintf(`[{
		"op": "remove",
		"path": "/processes/%s"
	}]`, t.ProcessID)), nil
}

func (t *processStopTransition) Validate(oldState SerializedState) error {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	// Make sure there is a matching process
	_, ok := oldNode.Processes[t.ProcessID]
	if !ok {
		return fmt.Errorf("process with id %s not found", t.ProcessID)
	}
	return nil
}

type addReverseProxyRuleTransition struct {
	Rule *ReverseProxyRule `json:"rule"`
}

func CreateAddReverseProxyRuleTransition(t ReverseProxyRuleType, matcher string, target string, appID string) Transition {
	return &addReverseProxyRuleTransition{
		Rule: &ReverseProxyRule{
			Type:    t,
			Matcher: matcher,
			Target:  target,
			AppID:   appID,
			ID:      uuid.New().String(),
		},
	}
}

func (t *addReverseProxyRuleTransition) Type() TransitionType {
	return TransitionAddReverseProxyRule
}

func (t *addReverseProxyRuleTransition) Patch(oldState SerializedState) (SerializedState, error) {
	marshaledRule, err := json.Marshal(t.Rule)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`[{
		"op": "add",
		"path": "/reverse_proxy_rules/%s",
		"value": %s
	}]`, t.Rule.ID, string(marshaledRule))), nil
}

func (t *addReverseProxyRuleTransition) Validate(oldState SerializedState) error {
	var oldNode NodeState
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	for _, rule := range oldNode.ReverseProxyRules {
		if rule.ID == t.Rule.ID {
			return fmt.Errorf("reverse proxy rule with id %s already exists", t.Rule.ID)
		}
		if rule.Matcher == t.Rule.Matcher {
			return fmt.Errorf("reverse proxy rule with matcher %s already exists", t.Rule.Matcher)
		}
	}

	return nil
}
