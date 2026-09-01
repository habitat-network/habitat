// Package server implements the HTTP handlers for network.habitat.opensocial.*
// and community.opensocial.* endpoints. All endpoints accept either an OAuth
// session or service-auth; org-scoped writes and admin reads additionally
// require the caller to hold the community's admin role.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/schema"

	"github.com/habitat-network/habitat/api/habitat"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
)

type Server struct {
	store     opensocial.Store
	validator authn.RequestValidator
	decoder   *schema.Decoder
}

func NewServer(store opensocial.Store, validator authn.RequestValidator) *Server {
	return &Server{
		store:     store,
		validator: validator,
		decoder:   schema.NewDecoder(),
	}
}

// CreateOrg implements network.habitat.opensocial.createOrg.
func (s *Server) CreateOrg(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input habitat.NetworkHabitatOpensocialCreateOrgInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	if input.Handle == "" {
		httpx.WriteInvalidRequest(ctx, w, "handle is required", nil)
		return
	}
	org, err := s.store.NewOrg(ctx, input.Handle, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("new org: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatOpensocialCreateOrgOutput{Org: org})
}

// UpdateProfile implements community.opensocial.updateProfile.
func (s *Server) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialUpdateProfileInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if input.Name == "" {
		httpx.WriteInvalidRequest(ctx, w, "name is required", nil)
		return
	}
	if !s.requireAdmin(ctx, w, org, credInfo.Subject) {
		return
	}
	if err := s.store.UpdateProfile(
		ctx, org, input.Name, input.Description, input.JoinPolicy,
	); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("update profile: %w", err))
		return
	}
}

// UploadImage implements community.opensocial.uploadImage: sets the
// community's profile avatar from the uploaded image bytes. Requires the
// caller to be an admin of the community.
func (s *Server) UploadImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params opensocial_api.CommunityOpensocialUploadImageParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, params.Org, "org")
	if !ok {
		return
	}
	if !s.requireAdmin(ctx, w, org, credInfo.Subject) {
		return
	}
	mimeType := r.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 500*1024))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		httpx.WriteError(ctx, w, "BlobTooLarge", "max 500kb", http.StatusRequestEntityTooLarge)
		return
	} else if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to read request body", err)
		return
	}
	blob, err := s.store.UploadImage(ctx, org, mimeType, data)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("upload image: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.CommunityOpensocialUploadImageOutput{Blob: blob})
}

func inviteToView(invite opensocial.Invite) opensocial_api.CommunityOpensocialDefsInviteView {
	return opensocial_api.CommunityOpensocialDefsInviteView{
		Id:        invite.ID,
		Org:       invite.Org.String(),
		Invitee:   invite.Invitee.String(),
		Roles:     invite.Roles,
		CreatedAt: invite.CreatedAt.Format(time.RFC3339),
	}
}

// requireAdmin validates that caller holds the community's admin role,
// writing an appropriate error response and returning false if not.
func (s *Server) requireAdmin(
	ctx context.Context,
	w http.ResponseWriter,
	org syntax.DID,
	caller syntax.DID,
) bool {
	roles, err := s.store.GetUserRoles(ctx, org, caller)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get user roles: %w", err))
		return false
	}
	if !slices.Contains(roles, opensocial.AdminRoleRkey) {
		httpx.WriteUnauthorized(ctx, w, "caller is not an admin of this community")
		return false
	}
	return true
}

// CreateInvite implements community.opensocial.createInvite.
func (s *Server) CreateInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialCreateInviteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if !s.requireAdmin(ctx, w, org, credInfo.Subject) {
		return
	}
	invitee, ok := httpx.ParseDIDInput(ctx, w, input.Invitee, "invitee")
	if !ok {
		return
	}
	invite, err := s.store.CreateInvite(ctx, org, invitee, input.Roles)
	if errors.Is(err, opensocial.ErrAlreadyMember) {
		httpx.WriteError(
			ctx, w, "AlreadyMember", "invitee is already a member", http.StatusBadRequest,
		)
		return
	}
	if errors.Is(err, opensocial.ErrInviteAlreadyExists) {
		httpx.WriteError(
			ctx, w, "InviteAlreadyExists",
			"invitee already has a pending invite", http.StatusBadRequest,
		)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("create invite: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.CommunityOpensocialCreateInviteOutput{
		Invite: inviteToView(invite),
	})
}

// ListInvites implements community.opensocial.listInvites.
func (s *Server) ListInvites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	invites, err := s.store.ListInvites(ctx, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list my invites: %w", err))
		return
	}
	views := make([]opensocial_api.CommunityOpensocialDefsInviteView, len(invites))
	for i, invite := range invites {
		views[i] = inviteToView(invite)
	}
	httpx.WriteJSON(ctx, w, opensocial_api.CommunityOpensocialListInvitesOutput{Invites: views})
}

// ListPendingInvites implements community.opensocial.listPendingInvites.
func (s *Server) ListPendingInvites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var params opensocial_api.CommunityOpensocialListPendingInvitesParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode query params", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, params.Org, "org")
	if !ok {
		return
	}
	if !s.requireAdmin(ctx, w, org, credInfo.Subject) {
		return
	}
	invites, err := s.store.ListPendingInvites(ctx, org)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list pending invites: %w", err))
		return
	}
	views := make([]opensocial_api.CommunityOpensocialDefsInviteView, len(invites))
	for i, invite := range invites {
		views[i] = inviteToView(invite)
	}
	httpx.WriteJSON(
		ctx, w, opensocial_api.CommunityOpensocialListPendingInvitesOutput{Invites: views},
	)
}

// RevokeInvite implements community.opensocial.revokeInvite.
func (s *Server) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialRevokeInviteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if !s.requireAdmin(ctx, w, org, credInfo.Subject) {
		return
	}
	if err := s.store.RevokeInvite(ctx, org, input.Id); err != nil {
		if errors.Is(err, opensocial.ErrInviteNotFound) {
			httpx.WriteError(
				ctx, w, "InviteNotFound",
				"no pending invite exists with this id", http.StatusNotFound,
			)
			return
		}
		httpx.WriteServerError(ctx, w, fmt.Errorf("revoke invite: %w", err))
		return
	}
}

// RequestJoin implements community.opensocial.requestJoin.
func (s *Server) RequestJoin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.CommunityOpensocialRequestJoinInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	roles, err := s.store.AcceptInvite(ctx, org, credInfo.Subject)
	if errors.Is(err, opensocial.ErrInviteNotFound) {
		httpx.WriteError(
			ctx, w, "InviteNotFound",
			"the calling user has no pending invite to this community", http.StatusNotFound,
		)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("accept invite: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, opensocial_api.CommunityOpensocialRequestJoinOutput{Roles: roles})
}
