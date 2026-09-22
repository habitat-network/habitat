// Package server exposes xrpc handlers for configuring org MCP servers and
// for members to connect/disconnect their credentials.
package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/mcpgateway"
	orgpkg "github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
)

// Server exposes xrpc handlers for the mcpgateway package.
type Server struct {
	store     mcpgateway.Store
	orgStore  orgpkg.Store
	validator authn.RequestValidator
}

// NewServer constructs a Server.
func NewServer(
	store mcpgateway.Store,
	orgStore orgpkg.Store,
	validator authn.RequestValidator,
) *Server {
	return &Server{store: store, orgStore: orgStore, validator: validator}
}

func toAPIServer(s *mcpgateway.Server) habitat.NetworkHabitatMcpDefsServer {
	return habitat.NetworkHabitatMcpDefsServer{
		Id:          string(s.ID),
		Name:        s.Name,
		Url:         s.URL,
		Description: s.Description,
		AuthType:    string(s.AuthType),
	}
}

func (s *Server) requireAdmin(
	w http.ResponseWriter,
	r *http.Request,
) (orgpkg.Org, bool) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return nil, false
	}

	org, err := s.orgStore.GetOrgForDID(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"getting organization",
			http.StatusInternalServerError,
		)
		return nil, false
	}

	isAdmin, err := org.IsAdmin(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking admin status",
			http.StatusInternalServerError,
		)
		return nil, false
	}
	if !isAdmin {
		w.WriteHeader(http.StatusUnauthorized)
		return nil, false
	}

	return org, true
}

func (s *Server) AddServer(w http.ResponseWriter, r *http.Request) {
	org, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatMcpAddServerInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Url == "" {
		utils.LogAndHTTPError(r.Context(), w, nil, "missing required fields", http.StatusBadRequest)
		return
	}

	server, err := s.store.AddServer(
		r.Context(),
		org.DID(),
		req.Name,
		req.Url,
		req.Description,
		mcpgateway.AuthType(req.AuthType),
	)
	if errors.Is(err, mcpgateway.ErrInvalidAuthType) {
		utils.LogAndHTTPError(r.Context(), w, err, "invalid auth type", http.StatusBadRequest)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "adding mcp server", http.StatusInternalServerError)
		return
	}

	httpx.WriteJSON(r.Context(), w, habitat.NetworkHabitatMcpAddServerOutput{
		Server: toAPIServer(server),
	})
}

func (s *Server) UpdateServer(w http.ResponseWriter, r *http.Request) {
	org, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatMcpUpdateServerInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}
	if req.Id == "" {
		utils.LogAndHTTPError(r.Context(), w, nil, "missing required fields", http.StatusBadRequest)
		return
	}

	var name, url, description *string
	var authType *mcpgateway.AuthType
	if req.Name != "" {
		name = &req.Name
	}
	if req.Url != "" {
		url = &req.Url
	}
	if req.Description != "" {
		description = &req.Description
	}
	if req.AuthType != "" {
		at := mcpgateway.AuthType(req.AuthType)
		authType = &at
	}

	server, err := s.store.UpdateServer(
		r.Context(),
		org.DID(),
		mcpgateway.ServerID(req.Id),
		name,
		url,
		description,
		authType,
	)
	if errors.Is(err, mcpgateway.ErrServerNotFound) {
		utils.LogAndHTTPError(r.Context(), w, err, "mcp server not found", http.StatusNotFound)
		return
	} else if errors.Is(err, mcpgateway.ErrInvalidAuthType) {
		utils.LogAndHTTPError(r.Context(), w, err, "invalid auth type", http.StatusBadRequest)
		return
	} else if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"updating mcp server",
			http.StatusInternalServerError,
		)
		return
	}

	httpx.WriteJSON(r.Context(), w, habitat.NetworkHabitatMcpUpdateServerOutput{
		Server: toAPIServer(server),
	})
}

func (s *Server) RemoveServer(w http.ResponseWriter, r *http.Request) {
	org, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatMcpRemoveServerInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}
	if req.Id == "" {
		utils.LogAndHTTPError(r.Context(), w, nil, "missing required fields", http.StatusBadRequest)
		return
	}

	err := s.store.RemoveServer(r.Context(), org.DID(), mcpgateway.ServerID(req.Id))
	if errors.Is(err, mcpgateway.ErrServerNotFound) {
		utils.LogAndHTTPError(r.Context(), w, err, "mcp server not found", http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"removing mcp server",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) ListServers(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	org, err := s.orgStore.GetOrgForDID(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"getting organization",
			http.StatusInternalServerError,
		)
		return
	}

	servers, err := s.store.ListServers(r.Context(), org.DID())
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"listing mcp servers",
			http.StatusInternalServerError,
		)
		return
	}

	out := make([]habitat.NetworkHabitatMcpListServersServerWithStatus, len(servers))
	for i, server := range servers {
		connected, err := s.store.IsConnected(r.Context(), credInfo.Subject, server.ID)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"checking mcp server connection",
				http.StatusInternalServerError,
			)
			return
		}
		out[i] = habitat.NetworkHabitatMcpListServersServerWithStatus{
			Server:    toAPIServer(server),
			Connected: connected,
		}
	}

	httpx.WriteJSON(r.Context(), w, habitat.NetworkHabitatMcpListServersOutput{
		Servers: out,
	})
}

func (s *Server) ConnectServer(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	org, err := s.orgStore.GetOrgForDID(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"getting organization",
			http.StatusInternalServerError,
		)
		return
	}

	var req habitat.NetworkHabitatMcpConnectServerInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}
	if req.Id == "" || req.Credential == "" {
		utils.LogAndHTTPError(r.Context(), w, nil, "missing required fields", http.StatusBadRequest)
		return
	}

	if _, err := s.store.GetServer(r.Context(), org.DID(), mcpgateway.ServerID(req.Id)); errors.Is(
		err,
		mcpgateway.ErrServerNotFound,
	) {
		utils.LogAndHTTPError(r.Context(), w, err, "mcp server not found", http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "getting mcp server", http.StatusInternalServerError)
		return
	}

	if err := s.store.ConnectServer(
		r.Context(),
		credInfo.Subject,
		mcpgateway.ServerID(req.Id),
		req.Credential,
	); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"connecting to mcp server",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) DisconnectServer(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatMcpDisconnectServerInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}
	if req.Id == "" {
		utils.LogAndHTTPError(r.Context(), w, nil, "missing required fields", http.StatusBadRequest)
		return
	}

	err := s.store.DisconnectServer(r.Context(), credInfo.Subject, mcpgateway.ServerID(req.Id))
	if errors.Is(err, mcpgateway.ErrCredentialNotFound) {
		utils.LogAndHTTPError(r.Context(), w, err, "not connected to mcp server", http.StatusNotFound)
		return
	} else if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"disconnecting from mcp server",
			http.StatusInternalServerError,
		)
		return
	}
}
