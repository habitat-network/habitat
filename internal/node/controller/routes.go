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

	pdsResp, err := h.nodeController.AddUser(req.UserID, req.Email, req.Handle, req.Password, req.Certificate)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	resp := types.PostAddUserResponse{
		PDSCreateAccountResponse: pdsResp,
	}
	respBody, err := json.Marshal(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(respBody)
}

// LoginRoute logs a user in, by proxying to the PDS com.atproto.server.createSession endpoint.
type LoginRoute struct {
	pdsClient PDSClientI
}

func NewLoginRoute(pdsClient PDSClientI) *LoginRoute {
	return &LoginRoute{
		pdsClient: pdsClient,
	}
}

func (h *LoginRoute) Pattern() string {
	return "/node/login"
}

func (h *LoginRoute) Method() string {
	return http.MethodPost
}

func (h *LoginRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req types.PDSCreateSessionRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	pdsResp, err := h.pdsClient.CreateSession(req.Identifier, req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respBody, err := json.Marshal(pdsResp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(respBody)
}
