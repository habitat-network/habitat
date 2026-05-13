package org

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xslices"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/utils"
)

// Serve org-specific APIs
// Server does both authn and authz for these routes
type Server struct {
	store Store
	auth  authn.Method
}

func NewServer(store Store, auth authn.Method) (*Server, error) {
	return &Server{
		store: store,
		auth:  auth,
	}, nil
}

// IsMember checks if the given DID is a member of any org on this instance.
func (s *Server) IsMember(ctx context.Context, member syntax.DID) (bool, error) {
	_, err := s.store.GetOrgForDID(ctx, member)
	if err != nil {
		return false, err
	}
	return true, nil
}

// Org APIs
func (s *Server) BootstrapAdmin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateOrg(w http.ResponseWriter, r *http.Request) {
	// no auth: bootstrapping a new org
	var req habitat.NetworkHabitatOrgCreateInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	if req.Subdomain == "" || req.AdminHandle == "" || req.AdminPassword == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	orgID, id, err := s.store.CreateOrg(r.Context(), req.Subdomain, req.Name, req.AdminHandle, req.AdminPassword)
	if errors.Is(err, identity.ErrInvalidHandle) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if errors.Is(err, ErrOrgAlreadyExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	} else if err != nil {
		utils.LogAndHTTPError(w, err, "creating organization", http.StatusInternalServerError)
		return
	}

	output := habitat.NetworkHabitatOrgCreateOutput{
		OrgId:       orgID,
		AdminDid:    id.DID.String(),
		AdminHandle: id.Handle.String(),
		Name:        req.Name,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
	}
}

func (s *Server) GetAdmins(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.auth)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting organization", http.StatusInternalServerError)
		return
	}

	dids, err := org.GetAdmins(r.Context())
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
	caller, ok := authn.Validate(w, r, s.auth)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting organization", http.StatusInternalServerError)
		return
	}

	dids, err := org.GetMembers(r.Context())
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
	caller, ok := authn.Validate(w, r, s.auth)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting organization", http.StatusInternalServerError)
		return
	}

	var req habitat.NetworkHabitatOrgAddAdminInput
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	admin, err := syntax.ParseAtIdentifier(req.Admin)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	ok, err = org.IsAdmin(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking admin status", http.StatusInternalServerError)
	}

	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	err = org.AddAdmin(r.Context(), admin.DID())
	if err != nil {
		utils.LogAndHTTPError(w, err, "adding admin", http.StatusInternalServerError)
	}
}

func (s *Server) RemoveAdmin(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.auth)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting organization", http.StatusInternalServerError)
		return
	}

	var req habitat.NetworkHabitatOrgRemoveAdminInput
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	admin, err := syntax.ParseAtIdentifier(req.Admin)
	if err != nil {
		utils.LogAndHTTPError(w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	ok, err = org.IsAdmin(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking admin status", http.StatusInternalServerError)
		return
	}

	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	err = org.RemoveAdmin(r.Context(), admin.DID())
	if err != nil {
		utils.LogAndHTTPError(w, err, "removing admin", http.StatusInternalServerError)
	}
}

func (s *Server) DowngradeAdmin(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.auth)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting organization", http.StatusInternalServerError)
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

	ok, err = org.IsAdmin(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "checking admin status", http.StatusInternalServerError)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if err = org.DowngradeAdmin(r.Context(), admin.DID()); err != nil {
		utils.LogAndHTTPError(w, err, "downgrading admin", http.StatusInternalServerError)
	}
}

func (s *Server) RemoveMembers(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.auth)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting organization", http.StatusInternalServerError)
		return
	}

	var req habitat.NetworkHabitatOrgRemoveMembersInput
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	ok, err = org.IsAdmin(r.Context(), caller)
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

	err = org.RemoveMembers(r.Context(), members)
	if err != nil {
		utils.LogAndHTTPError(w, err, "removing members", http.StatusInternalServerError)
	}
}

func (s *Server) GetMetadata(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.auth)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting organization", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(org.GetMetadata()); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) IssueInviteToken(w http.ResponseWriter, r *http.Request) {
	caller, ok := authn.Validate(w, r, s.auth)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), caller)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting organization", http.StatusInternalServerError)
		return
	}

	var req habitat.NetworkHabitatOrgIssueInviteTokenInput
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	expiresAt := time.Now().AddDate(0, 0, 7)
	if req.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, req.ExpiresAt)
		if err != nil {
			utils.LogAndHTTPError(w, err, "parsing expiresAt", http.StatusBadRequest)
			return
		}
		expiresAt = parsed
	}

	token, err := org.IssueIdentityToken(r.Context(), caller, req.Reusable, expiresAt)
	if err != nil {
		utils.LogAndHTTPError(w, err, "generating identity token", http.StatusInternalServerError)
		return
	}

	output := habitat.NetworkHabitatOrgIssueInviteTokenOutput{
		Token: token,
	}
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) MintMemberIdentity(w http.ResponseWriter, r *http.Request) {
	// no authn/authz: this is called by new members who don't exist yet
	var req habitat.NetworkHabitatOrgMintMemberIdentityInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(w, err, "reading request body", http.StatusBadRequest)
		return
	}

	if req.Token == "" || req.Handle == "" || req.Password == "" || req.OrgId == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	org, err := s.store.GetOrg(r.Context(), req.OrgId)
	if err != nil {
		utils.LogAndHTTPError(w, err, "getting organization", http.StatusInternalServerError)
		return
	}

	id, err := org.CreateNewMemberIdentity(r.Context(), req.Token, req.Handle, req.Password)
	if errors.Is(err, ErrInvalidToken) {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	} else if err != nil {
		utils.LogAndHTTPError(w, err, "minting member identity", http.StatusInternalServerError)
		return
	}

	output := habitat.NetworkHabitatOrgMintMemberIdentityOutput{
		Did:    id.DID.String(),
		Handle: id.Handle.String(),
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(w, err, "encoding response", http.StatusInternalServerError)
		return
	}
}
