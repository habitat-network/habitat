package controller

import (
	"encoding/json"
	"fmt"
	"net/http"

	types "github.com/eagraf/habitat-new/core/api"
	"github.com/eagraf/habitat-new/internal/node/constants"
	"github.com/eagraf/habitat-new/internal/node/hdb"
	"golang.org/x/mod/semver"
)

// MigrationRoute calls nodeController.Migrate()
type MigrationRoute struct {
	nodeController NodeController
}

func NewMigrationRoute(nodeController NodeController) *MigrationRoute {
	return &MigrationRoute{
		nodeController: nodeController,
	}
}

func (h *MigrationRoute) Pattern() string {
	return "/node/migrate"
}

func (h *MigrationRoute) Method() string {
	return http.MethodPost
}

func (h *MigrationRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req types.MigrateRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate semver
	valid := semver.IsValid(req.TargetVersion)
	if !valid {
		http.Error(w, fmt.Sprintf("invalid semver %s", req.TargetVersion), http.StatusBadRequest)
		return
	}

	err = h.nodeController.MigrateNodeDB(req.TargetVersion)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// InstallAppRoute calls nodeController.InstallApp()
type InstallAppRoute struct {
	nodeController NodeController
}

func NewInstallAppRoute(nodeController NodeController) *InstallAppRoute {
	return &InstallAppRoute{
		nodeController: nodeController,
	}
}

func (h *InstallAppRoute) Pattern() string {
	return "/node/users/{user_id}/apps"
}

func (h *InstallAppRoute) Method() string {
	return http.MethodPost
}

func (h *InstallAppRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")

	var req types.PostAppRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	appInstallation := req.AppInstallation

	err = h.nodeController.InstallApp(userID, appInstallation, req.ReverseProxyRules)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// TODO validate request
	w.WriteHeader(http.StatusCreated)
}

type StartProcessHandler struct {
	nodeController NodeController
}

func NewStartProcessHandler(nodeController NodeController) *StartProcessHandler {
	return &StartProcessHandler{
		nodeController: nodeController,
	}
}

func (h *StartProcessHandler) Pattern() string {
	return "/node/processes"
}

func (h *StartProcessHandler) Method() string {
	return http.MethodPost
}

func (h *StartProcessHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req types.PostProcessRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	app, err := h.nodeController.GetAppByID(req.AppInstallationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = h.nodeController.StartProcess(app.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// GetNodeRoute gets the node's database and returns its state map.
type GetNodeRoute struct {
	dbManager hdb.HDBManager
}

func NewGetNodeRoute(dbManager hdb.HDBManager) *GetNodeRoute {
	return &GetNodeRoute{
		dbManager: dbManager,
	}
}

func (h *GetNodeRoute) Pattern() string {
	return "/node"
}

func (h *GetNodeRoute) Method() string {
	return http.MethodGet
}

func (h *GetNodeRoute) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	dbClient, err := h.dbManager.GetDatabaseClientByName(constants.NodeDBDefaultName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	stateBytes := dbClient.Bytes()
	var stateMap map[string]interface{}
	err = json.Unmarshal(stateBytes, &stateMap)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := types.GetDatabaseResponse{
		DatabaseID: dbClient.DatabaseID(),
		State:      stateMap,
	}

	respBody, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(respBody)
}

// AddUserRoute calls nodeController.AddUser()
type AddUserRoute struct {
	nodeController NodeController
}

func NewAddUserRoute(nodeController NodeController) *AddUserRoute {
	return &AddUserRoute{
		nodeController: nodeController,
	}
}

func (h *AddUserRoute) Pattern() string {
	return "/node/users"
}

func (h *AddUserRoute) Method() string {
	return http.MethodPost
}

func (h *AddUserRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req types.PostAddUserRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = h.nodeController.AddUser(req.UserID, req.Username, req.Certificate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// TODO validate request
	w.WriteHeader(http.StatusCreated)
}
