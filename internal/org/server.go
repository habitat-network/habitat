package org

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xslices"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/utils"
)

// Serve org-specific APIs
// Server does both authn and authz for these routes
type Server struct {
	// domain where this org is hosted
	domain string

	org  Org
	auth authn.Method
}

func NewServer(domain string, org Org, auth authn.Method) (*Server, error) {
	return &Server{
		org:  org,
		auth: auth,
	}, nil
}

// IsMember implements Org so that *Server can be used wherever an Org is expected.
func (s *Server) IsMember(ctx context.Context, member syntax.DID) (bool, error) {
	return s.org.IsMember(ctx, member)
}

func (s *Server) authnWithOrg(w http.ResponseWriter, r *http.Request, authnMethod ...authn.Method) (syntax.DID, bool) {
	callerDID, ok := authn.Validate(w, r, authnMethod...)

	// If unable to authenticate, return false
	if !ok {
		return "", false
	}

	// Otherwise, only authn if part of org
	ok, err := s.org.IsMember(r.Context(), callerDID)
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking org.isMember", http.StatusInternalServerError)
		return "", false
	}
	if !ok {
		http.Error(w, "not a member of this org", http.StatusUnauthorized)
		return "", false
	}

	return callerDID, true
}

// Org APIs
func (s *Server) BootstrapAdmin(w http.ResponseWriter, r *http.Request) {
	// TODO: implement once we have a provisioner process; til then this is manual
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) GetAdmins(w http.ResponseWriter, r *http.Request) {
	_, ok := s.authnWithOrg(w, r, s.auth)
	if !ok {
		return
	}

	dids, err := s.org.GetAdmins(r.Context())
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting org members", http.StatusInternalServerError)
	}

	admins := xslices.Map(dids, func(m syntax.DID) string {
		return m.String()
	})

	if err = json.NewEncoder(w).Encode(&habitat.NetworkHabitatOrgGetAdminsOutput{
		Admins: admins,
	}); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) GetMembers(w http.ResponseWriter, r *http.Request) {
	_, ok := s.authnWithOrg(w, r, s.auth)
	if !ok {
		return
	}

	dids, err := s.org.GetMembers(r.Context())
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting org members", http.StatusInternalServerError)
	}

	members := xslices.Map(dids, func(m syntax.DID) string {
		return m.String()
	})

	if err = json.NewEncoder(w).Encode(&habitat.NetworkHabitatOrgGetMembersOutput{
		Members: members,
	}); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) AddAdmin(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authnWithOrg(w, r, s.auth)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatOrgAddAdminInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	admin, err := syntax.ParseAtIdentifier(req.Admin)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	// authz: only admin can add admin
	ok, err = s.org.IsAdmin(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking admin status", http.StatusInternalServerError)
	}

	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	err = s.org.AddAdmin(r.Context(), admin.DID())
	if err != nil {
		utils.LogAndHTTPError(w, err, "adding admin", http.StatusInternalServerError)
	}
}

func (s *Server) AddMembers(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authnWithOrg(w, r, s.auth)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatOrgAddMembersInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	// authz: only admin can add members
	ok, err = s.org.IsAdmin(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking admin status", http.StatusInternalServerError)
		return
	}

	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	members := make([]syntax.DID, 0, len(req.Members))
	for _, m := range req.Members {
		id, err := syntax.ParseAtIdentifier(m)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
			return
		}
		members = append(members, id.DID())
	}

	err = s.org.AddMembers(r.Context(), members)
	if err != nil {
		utils.LogAndHTTPError(w, err, "adding members", http.StatusInternalServerError)
	}
}

func (s *Server) RemoveAdmin(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authnWithOrg(w, r, s.auth)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatOrgRemoveAdminInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	admin, err := syntax.ParseAtIdentifier(req.Admin)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	// authz: only admin can remove admin
	ok, err = s.org.IsAdmin(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking admin status", http.StatusInternalServerError)
		return
	}

	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	err = s.org.RemoveAdmin(r.Context(), admin.DID())
	if err != nil {
		utils.LogAndHTTPError(w, err, "removing admin", http.StatusInternalServerError)
	}
}

func (s *Server) DowngradeAdmin(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authnWithOrg(w, r, s.oauth)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatOrgDowngradeAdminInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	admin, err := syntax.ParseAtIdentifier(req.Admin)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	ok, err = s.org.IsAdmin(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking admin status", http.StatusInternalServerError)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if err = s.org.DowngradeAdmin(r.Context(), admin.DID()); err != nil {
		utils.LogAndHTTPError(w, err, "downgrading admin", http.StatusInternalServerError)
	}
}

func (s *Server) RemoveMembers(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authnWithOrg(w, r, s.auth)
	if !ok {
		return
	}

	var req habitat.NetworkHabitatOrgRemoveMembersInput
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	// authz: only admin can remove members
	ok, err = s.org.IsAdmin(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking admin status", http.StatusInternalServerError)
		return
	}

	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	members := make([]syntax.DID, 0, len(req.Members))
	for _, m := range req.Members {
		id, err := syntax.ParseAtIdentifier(m)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
			return
		}
		members = append(members, id.DID())
	}

	err = s.org.RemoveMembers(r.Context(), members)
	if err != nil {
		utils.LogAndHTTPError(w, err, "removing members", http.StatusInternalServerError)
	}
}

// TODO: figure out a way to configure / store more metadata about the org
func (s *Server) GetMetadata(w http.ResponseWriter, r *http.Request) {
	_, ok := s.authnWithOrg(w, r, s.oauth)
	if !ok {
		return
	}

	if err := json.NewEncoder(w).Encode(s.org.GetMetadata()); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}
