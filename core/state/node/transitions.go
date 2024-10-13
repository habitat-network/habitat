package node

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eagraf/habitat-new/internal/node/hdb"
	"github.com/google/uuid"
)

var (
	TransitionInitialize          = "initialize"
	TransitionMigrationUp         = "migration_up"
	TransitionAddUser             = "add_user"
	TransitionStartInstallation   = "start_installation"
	TransitionFinishInstallation  = "finish_installation"
	TransitionStartUninstallation = "start_uninstallation"
	TransitionStartProcess        = "process_start"
	TransitionProcessRunning      = "process_running"
	TransitionStopProcess         = "process_stop"
	TransitionAddReverseProxyRule = "add_reverse_proxy_rule"
)

type InitalizationTransition struct {
	InitState *State `json:"init_state"`
}

func (t *InitalizationTransition) Type() string {
	return TransitionInitialize
}

func (t *InitalizationTransition) Patch(oldState hdb.SerializedState) (hdb.SerializedState, error) {
	if t.InitState.Users == nil {
		t.InitState.Users = make(map[string]*User, 0)
	}

	if t.InitState.AppInstallations == nil {
		t.InitState.AppInstallations = make(map[string]*AppInstallationState)
	}

	if t.InitState.Processes == nil {
		t.InitState.Processes = make(map[string]*ProcessState)
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

func (t *InitalizationTransition) Enrich(oldState hdb.SerializedState) error {
	return nil
}

func (t *InitalizationTransition) Validate(oldState hdb.SerializedState) error {
	if t.InitState == nil {
		return fmt.Errorf("init state cannot be nil")
	}
	return nil
}

type MigrationTransition struct {
	TargetVersion string
}

func (t *MigrationTransition) Type() string {
	return TransitionMigrationUp
}

func (t *MigrationTransition) Patch(oldState hdb.SerializedState) (hdb.SerializedState, error) {
	var oldNode State
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

func (t *MigrationTransition) Enrich(oldState hdb.SerializedState) error {
	return nil
}

func (t *MigrationTransition) Validate(oldState hdb.SerializedState) error {
	var oldNode State
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

type AddUserTransition struct {
	Username    string `json:"username"`
	Certificate string `json:"certificate"`
	AtprotoDID  string `json:"atproto_did"`

	EnrichedData *AddUserTranstitionEnrichedData `json:"enriched_data"`
}

type AddUserTranstitionEnrichedData struct {
	User *User `json:"user"`
}

func (t *AddUserTransition) Type() string {
	return TransitionAddUser
}

func (t *AddUserTransition) Patch(oldState hdb.SerializedState) (hdb.SerializedState, error) {

	user, err := json.Marshal(t.EnrichedData.User)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf(`[{
		"op": "add",
		"path": "/users/%s",
		"value": %s
	}]`, t.EnrichedData.User.ID, user)), nil
}

func (t *AddUserTransition) Enrich(oldState hdb.SerializedState) error {

	id := uuid.New().String()

	t.EnrichedData = &AddUserTranstitionEnrichedData{
		User: &User{
			ID:          id,
			Username:    t.Username,
			Certificate: t.Certificate,
			AtprotoDID:  t.AtprotoDID,
		},
	}
	return nil
}

func (t *AddUserTransition) Validate(oldState hdb.SerializedState) error {

	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	_, ok := oldNode.Users[t.EnrichedData.User.ID]
	if ok {
		return fmt.Errorf("user with id %s already exists", t.EnrichedData.User.ID)
	}

	// Check for conflicting usernames
	for _, user := range oldNode.Users {
		if user.Username == t.Username {
			return fmt.Errorf("user with username %s already exists", t.Username)
		}
	}
	return nil
}

type StartInstallationTransition struct {
	UserID                 string `json:"user_id"`
	StartAfterInstallation bool   `json:"start_after_installation"`

	*AppInstallation
	NewProxyRules []*ReverseProxyRule                      `json:"new_proxy_rules"`
	EnrichedData  *StartInstallationTransitionEnrichedData `json:"enriched_data"`
}

type StartInstallationTransitionEnrichedData struct {
	AppState      *AppInstallationState `json:"app_state"`
	NewProxyRules []*ReverseProxyRule   `json:"new_proxy_rules"`
}

func (t *StartInstallationTransition) Type() string {
	return TransitionStartInstallation
}

func (t *StartInstallationTransition) Patch(oldState hdb.SerializedState) (hdb.SerializedState, error) {
	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return nil, err
	}

	marshalledApp, err := json.Marshal(t.EnrichedData.AppState)
	if err != nil {
		return nil, err
	}

	_, ok := oldNode.Users[t.UserID]
	if !ok {
		return nil, fmt.Errorf("user with id %s not found", t.UserID)
	}

	marshaledRules := make([]string, 0)
	for _, rule := range t.EnrichedData.NewProxyRules {
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
	]`, t.EnrichedData.AppState.ID, string(marshalledApp), rules)), nil
}

func (t *StartInstallationTransition) Enrich(oldState hdb.SerializedState) error {
	id := uuid.New().String()
	appInstallState := &AppInstallationState{
		AppInstallation: &AppInstallation{
			ID:      id,
			UserID:  t.UserID,
			Name:    t.Name,
			Version: t.Version,
			Package: t.Package,
		},
		State: AppLifecycleStateInstalling,
	}

	enrichedRules := make([]*ReverseProxyRule, 0)

	for _, rule := range t.NewProxyRules {
		enrichedRules = append(enrichedRules, &ReverseProxyRule{
			ID:      uuid.New().String(),
			AppID:   appInstallState.ID,
			Type:    rule.Type,
			Matcher: rule.Matcher,
			Target:  rule.Target,
		})
		rule.ID = uuid.New().String()
		rule.AppID = appInstallState.ID
	}

	t.EnrichedData = &StartInstallationTransitionEnrichedData{
		AppState:      appInstallState,
		NewProxyRules: enrichedRules,
	}
	return nil
}

func (t *StartInstallationTransition) Validate(oldState hdb.SerializedState) error {
	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	_, ok := oldNode.Users[t.UserID]
	if !ok {
		return fmt.Errorf("user with id %s not found", t.UserID)
	}

	app, ok := oldNode.AppInstallations[t.EnrichedData.AppState.ID]
	if ok {
		if app.Version == t.Version {
			return fmt.Errorf("app %s version %s for user %s found in state %s", t.Name, t.Version, t.UserID, app.State)
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

type FinishInstallationTransition struct {
	UserID          string `json:"user_id"`
	AppID           string `json:"app_id"`
	RegistryURLBase string `json:"registry_url_base"`
	RegistryAppID   string `json:"registry_app_id"`

	StartAfterInstallation bool `json:"start_after_installation"`
}

func (t *FinishInstallationTransition) Type() string {
	return TransitionFinishInstallation
}

func (t *FinishInstallationTransition) Patch(oldState hdb.SerializedState) (hdb.SerializedState, error) {
	var oldNode State
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

func (t *FinishInstallationTransition) Enrich(oldState hdb.SerializedState) error {
	return nil
}

func (t *FinishInstallationTransition) Validate(oldState hdb.SerializedState) error {
	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	app, ok := oldNode.AppInstallations[t.AppID]
	if !ok {
		return fmt.Errorf("app with id %s not found", t.AppID)
	}

	_, ok = oldNode.Users[t.UserID]
	if !ok {
		return fmt.Errorf("user with id %s not found", t.UserID)
	}

	if app.RegistryURLBase != t.RegistryURLBase || app.RegistryPackageID != t.RegistryAppID {
		return fmt.Errorf("app %s for user %s found in state with different registry url %s and package id %s", app.Name, t.UserID, app.RegistryURLBase, app.RegistryPackageID)
	}

	if app.State != "installing" {
		return fmt.Errorf("app %s for user %s is in state %s", app.Name, t.UserID, app.State)
	}

	return nil
}

// TODO handle uninstallation

type ProcessStartTransition struct {
	// Requested data
	AppID string

	EnrichedData *ProcessStartTransitionEnrichedData
}

type ProcessStartTransitionEnrichedData struct {
	Process *ProcessState
	App     *AppInstallationState
}

func (t *ProcessStartTransition) Type() string {
	return TransitionStartProcess
}

func (t *ProcessStartTransition) Patch(oldState hdb.SerializedState) (hdb.SerializedState, error) {
	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return nil, err
	}

	marshaled, err := json.Marshal(t.EnrichedData.Process)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf(`[{
			"op": "add",
			"path": "/processes/%s",
			"value": %s
		}]`, t.EnrichedData.Process.ID, marshaled)), nil
}

func (t *ProcessStartTransition) Enrich(oldState hdb.SerializedState) error {
	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	app, err := oldNode.GetAppByID(t.AppID)
	if err != nil {
		return err
	}

	proc := &ProcessState{
		Process: &Process{
			UserID: app.UserID,
			Driver: app.Driver,
			AppID:  app.ID,
		},
		State:       ProcessStateStarting,
		ExtDriverID: "", // this should not be set yet
	}

	proc.ID = uuid.New().String()
	proc.Process.Created = time.Now().Format(time.RFC3339)

	t.EnrichedData = &ProcessStartTransitionEnrichedData{
		Process: proc,
		App:     app,
	}
	return nil
}

func (t *ProcessStartTransition) Validate(oldState hdb.SerializedState) error {

	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	if t.EnrichedData.Process == nil {
		return fmt.Errorf("process was not properly enriched")
	}

	if t.EnrichedData.App == nil {
		return fmt.Errorf("app was not properly enriched")
	}

	// Make sure the app installation is in the installed state
	userID := t.EnrichedData.Process.UserID
	if t.EnrichedData.App.State != AppLifecycleStateInstalled {
		return fmt.Errorf("app with id %s for user %s is not in state %s", t.AppID, userID, AppLifecycleStateInstalled)
	}

	// Check user exists
	_, ok := oldNode.Users[t.EnrichedData.Process.UserID]
	if !ok {
		return fmt.Errorf("user with id %s does not exist", userID)
	}
	if _, ok := oldNode.Processes[t.EnrichedData.Process.ID]; ok {
		return fmt.Errorf("Process with id %s already exists", t.EnrichedData.Process.ID)
	}

	for _, proc := range oldNode.Processes {
		// Make sure that no app with the same ID has a process
		if proc.AppID == t.AppID {
			return fmt.Errorf("app with id %s already has a process", t.AppID)
		}
	}

	return nil
}

type ProcessRunningTransition struct {
	ProcessID string `json:"process_id"`
}

func (t *ProcessRunningTransition) Type() string {
	return TransitionProcessRunning
}

func (t *ProcessRunningTransition) Patch(oldState hdb.SerializedState) (hdb.SerializedState, error) {
	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return nil, err
	}

	// Find the matching process
	proc, ok := oldNode.Processes[t.ProcessID]
	if !ok {
		return nil, fmt.Errorf("process with id %s not found", t.ProcessID)
	}
	proc.State = ProcessStateRunning

	marshaled, err := json.Marshal(proc)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf(`[{
		"op": "replace",
		"path": "/processes/%s",
		"value": %s
	}]`, t.ProcessID, marshaled)), nil
}

func (t *ProcessRunningTransition) Enrich(oldState hdb.SerializedState) error {
	return nil
}

func (t *ProcessRunningTransition) Validate(oldState hdb.SerializedState) error {
	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	// Make sure there is a matching process
	proc, ok := oldNode.Processes[t.ProcessID]
	if !ok {
		return fmt.Errorf("process with id %s not found", t.ProcessID)
	}
	if proc.State != ProcessStateStarting {
		return fmt.Errorf("Process with id %s is in state %s, must be in state %s", t.ProcessID, proc.State, ProcessStateStarting)
	}

	return nil
}

type ProcessStopTransition struct {
	ProcessID string `json:"process_id"`
}

func (t *ProcessStopTransition) Type() string {
	return TransitionStopProcess
}

func (t *ProcessStopTransition) Patch(oldState hdb.SerializedState) (hdb.SerializedState, error) {
	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return nil, err
	}

	_, ok := oldNode.Processes[t.ProcessID]
	if !ok {
		return nil, fmt.Errorf("process with id %s not found", t.ProcessID)
	}

	return []byte(fmt.Sprintf(`[{
		"op": "remove",
		"path": "/processes/%s"
	}]`, t.ProcessID)), nil
}

func (t *ProcessStopTransition) Enrich(oldState hdb.SerializedState) error {
	return nil
}

func (t *ProcessStopTransition) Validate(oldState hdb.SerializedState) error {
	var oldNode State
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

type AddReverseProxyRuleTransition struct {
	Rule *ReverseProxyRule `json:"rule"`
}

func (t *AddReverseProxyRuleTransition) Type() string {
	return TransitionAddReverseProxyRule
}

func (t *AddReverseProxyRuleTransition) Patch(oldState hdb.SerializedState) (hdb.SerializedState, error) {

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

func (t *AddReverseProxyRuleTransition) Enrich(oldState hdb.SerializedState) error {
	if t.Rule.ID == "" {
		t.Rule.ID = uuid.New().String()
	}

	return nil
}

func (t *AddReverseProxyRuleTransition) Validate(oldState hdb.SerializedState) error {
	var oldNode State
	err := json.Unmarshal(oldState, &oldNode)
	if err != nil {
		return err
	}

	for _, rule := range *oldNode.ReverseProxyRules {
		if rule.ID == t.Rule.ID {
			return fmt.Errorf("reverse proxy rule with id %s already exists", t.Rule.ID)
		}
		if rule.Matcher == t.Rule.Matcher {
			return fmt.Errorf("reverse proxy rule with matcher %s already exists", t.Rule.Matcher)
		}
	}

	return nil
}
