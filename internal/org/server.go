package org

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/schema"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/core"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/utils"
)

var errNotMemberOfOrg = errors.New("not a member of an organization")

// Serve org-specific APIs
// Server does both authn and authz for these routes
type Server struct {
	store          Store
	auth           authn.Method
	pear           pear.Pear
	domain         string
	decoder        *schema.Decoder
	dir            identity.Directory
	instancePolicy instance.PolicyStore
}

func NewServer(
	store Store,
	auth authn.Method,
	p pear.Pear,
	domain string,
	dir identity.Directory,
	instancePolicy instance.PolicyStore,
) (*Server, error) {
	return &Server{
		store:          store,
		auth:           auth,
		pear:           p,
		domain:         domain,
		decoder:        schema.NewDecoder(),
		dir:            dir,
		instancePolicy: instancePolicy,
	}, nil
}

// IsMember checks if the given DID is a member of any org on this instance.
func (s *Server) IsMember(ctx context.Context, member syntax.DID) (bool, error) {
	_, _, err := s.store.GetOrgForDID(ctx, member)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) validateOrgToken(
	ctx context.Context,
	orgID string,
	token string,
) (core.Org, error) {
	org, err := s.store.GetOrg(ctx, syntax.DID(orgID))
	if err != nil {
		return nil, err
	}

	// Org found, validate token.
	if err := s.store.ValidateAdminSignedToken(ctx, syntax.DID(orgID), token); err != nil {
		return nil, err
	}

	return org, nil
}

func (s *Server) GetMetadata(w http.ResponseWriter, r *http.Request) {
	var params habitat.NetworkHabitatOrgGetMetadataParams
	err := s.decoder.Decode(&params, r.URL.Query())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing url", http.StatusBadRequest)
		return
	}

	orgID := params.OrgId
	var org core.Org
	// Either orgID is supplied in query params and the signed token is passed up for authn method
	if orgID != "" {
		org, err = s.validateOrgToken(
			r.Context(),
			orgID,
			strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
		)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "unauthorized", http.StatusUnauthorized)
			return
		}
	} else {
		// Or regular authn via authenticated credInfo.Subject
		credInfo, ok := authn.NewValidator(authn.WithAuthMethods(s.auth)).Validate(w, r)
		if !ok {
			return
		}

		org, _, err = s.store.GetOrgForDID(r.Context(), credInfo.Subject)
		if errors.Is(err, ErrMemberNotFound) {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				errNotMemberOfOrg.Error(),
				http.StatusNotFound,
			)
			return
		} else if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"getting organization",
				http.StatusInternalServerError,
			)
			return
		}

	}

	meta := org.GetMetadata(r.Context(), s.domain)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
	}
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
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	if req.AdminHandle == "" || req.Name == "" || req.LoginMethod == "" || req.ContactEmail == "" {
		utils.LogAndHTTPError(r.Context(), w, nil, "missing required fields", http.StatusBadRequest)
		return
	}

	if _, err := mail.ParseAddress(req.ContactEmail); err != nil {
		utils.LogAndHTTPError(r.Context(), w, nil, "invalid contact email", http.StatusBadRequest)
		return
	}

	if req.LoginMethod == "password" && req.AdminPassword == "" {
		utils.LogAndHTTPError(r.Context(), w, nil, "missing required fields", http.StatusBadRequest)
		return
	} else if req.LoginMethod != "password" && req.LoginId == "" {
		utils.LogAndHTTPError(r.Context(), w, nil, "missing required fields", http.StatusBadRequest)
		return
	}

	policy, err := s.instancePolicy.GetOrgCreationPolicy(r.Context())
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking org creation policy",
			http.StatusInternalServerError,
		)
		return
	}
	inviteOnly := policy == "invite_only"
	if inviteOnly {
		if req.InviteToken == "" {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				errors.New("an invite link is required to create an org on this instance"),
				"checking org creation policy",
				http.StatusForbidden,
			)
			return
		}
		if err := s.instancePolicy.ValidateInvite(r.Context(), req.InviteToken); err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				errors.New("an invite link is required to create an org on this instance"),
				"validating invite",
				http.StatusForbidden,
			)
			return
		}
	}

	orgID, id, err := s.store.CreateOrg(
		r.Context(),
		req.Name,
		req.AdminHandle,
		req.AdminPassword,
		req.LoginMethod,
		req.LoginId,
		req.HandleSubdomain,
		req.ContactEmail,
	)
	if errors.Is(err, identity.ErrInvalidHandle) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	} else if errors.Is(err, ErrOrgAlreadyExists) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	} else if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"creating organization",
			http.StatusInternalServerError,
		)
		return
	}

	// The org now exists. If an invite gated this creation, mark it used now
	// (not before creation) so a failed attempt doesn't permanently burn a
	// single-use invite. If marking-used fails here, the org was still
	// created successfully, so we log but do not fail the response.
	if inviteOnly {
		if err := s.instancePolicy.MarkInviteUsed(r.Context(), req.InviteToken); err != nil {
			slog.ErrorContext(
				r.Context(),
				"failed to mark invite as used after org creation succeeded",
				"err", err,
			)
		}
	}

	output := habitat.NetworkHabitatOrgCreateOutput{
		OrgId:       orgID.DID.String(),
		AdminDid:    id.DID.String(),
		AdminHandle: id.Handle.String(),
		Name:        req.Name,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
	}
}

func (s *Server) GetAdmins(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.auth),
		authn.WithSupportedCredentials(authn.OrgCredential, authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	org, _, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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

	dids, err := org.GetAdmins(r.Context())
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"getting org members",
			http.StatusInternalServerError,
		)
	}

	admins := make([]habitat.NetworkHabitatOrgGetAdminsMember, len(dids))
	for i, did := range dids {
		id, err := s.dir.LookupDID(context.Background(), did)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"looking up org admins",
				http.StatusInternalServerError,
			)
			return
		}
		admins[i] = habitat.NetworkHabitatOrgGetAdminsMember{
			Did:    did.String(),
			Handle: id.Handle.String(),
		}
	}

	if err = json.NewEncoder(w).Encode(&habitat.NetworkHabitatOrgGetAdminsOutput{
		Admins: admins,
	}); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) GetMembers(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.auth),
		authn.WithSupportedCredentials(authn.OrgCredential, authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	org, _, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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

	dids, err := org.GetMembers(r.Context())
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"getting org members",
			http.StatusInternalServerError,
		)
	}

	members := make([]habitat.NetworkHabitatOrgGetMembersMember, len(dids))
	for i, did := range dids {
		id, err := s.dir.LookupDID(context.Background(), did)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"looking up org admins",
				http.StatusInternalServerError,
			)
			return
		}
		members[i] = habitat.NetworkHabitatOrgGetMembersMember{
			Did:    did.String(),
			Handle: id.Handle.String(),
		}
	}

	if err = json.NewEncoder(w).Encode(&habitat.NetworkHabitatOrgGetMembersOutput{
		Members: members,
	}); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) AddAdmin(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.auth),
		authn.WithSupportedCredentials(authn.OrgCredential, authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	org, _, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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

	var req habitat.NetworkHabitatOrgAddAdminInput
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	admin, err := syntax.ParseAtIdentifier(req.Admin)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	ok, err = org.IsAdmin(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking admin status",
			http.StatusInternalServerError,
		)
	}

	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	err = org.AddAdmin(r.Context(), admin.DID())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "adding admin", http.StatusInternalServerError)
	}
}

func (s *Server) RemoveAdmin(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.auth),
		authn.WithSupportedCredentials(authn.OrgCredential, authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	org, _, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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

	var req habitat.NetworkHabitatOrgRemoveAdminInput
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	admin, err := syntax.ParseAtIdentifier(req.Admin)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	ok, err = org.IsAdmin(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking admin status",
			http.StatusInternalServerError,
		)
		return
	}

	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	err = org.RemoveAdmin(r.Context(), admin.DID())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "removing admin", http.StatusInternalServerError)
	}
}

func (s *Server) DowngradeAdmin(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.auth),
		authn.WithSupportedCredentials(authn.OrgCredential, authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	org, _, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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

	var req habitat.NetworkHabitatOrgDowngradeAdminInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	admin, err := syntax.ParseAtIdentifier(req.Admin)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing at identifier", http.StatusBadRequest)
		return
	}

	ok, err = org.IsAdmin(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking admin status",
			http.StatusInternalServerError,
		)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	if err = org.DowngradeAdmin(r.Context(), admin.DID()); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"downgrading admin",
			http.StatusInternalServerError,
		)
	}
}

func (s *Server) RemoveMembers(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.auth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	org, _, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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

	var req habitat.NetworkHabitatOrgRemoveMembersInput
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	ok, err = org.IsAdmin(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking admin status",
			http.StatusInternalServerError,
		)
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
			utils.LogAndHTTPError(
				r.Context(),
				w,
				err,
				"parsing at identifier",
				http.StatusBadRequest,
			)
			return
		}
		members = append(members, id.DID())
	}

	err = org.RemoveMembers(r.Context(), members)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"removing members",
			http.StatusInternalServerError,
		)
	}
}

func (s *Server) IssueInviteToken(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := authn.NewValidator(
		authn.WithAuthMethods(s.auth),
		authn.WithSupportedCredentials(authn.UserCredential),
	).Validate(w, r)
	if !ok {
		return
	}

	org, _, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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

	// authz: calelr must be admin
	if ok, err := org.IsAdmin(r.Context(), credInfo.Subject); !ok {
		utils.LogAndHTTPError(r.Context(), w, err, "not authorized", http.StatusUnauthorized)
		return
	} else if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"checking IsAdmin",
			http.StatusInternalServerError,
		)
		return
	}

	var req habitat.NetworkHabitatOrgIssueInviteTokenInput
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	expiresAt := time.Now().AddDate(0, 0, 7)
	if req.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, req.ExpiresAt)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parsing expiresAt", http.StatusBadRequest)
			return
		}
		expiresAt = parsed
	}

	token, err := s.store.IssueIdentityToken(
		r.Context(),
		org.DID(),
		credInfo.Subject,
		req.Reusable,
		expiresAt,
	)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"generating identity token",
			http.StatusInternalServerError,
		)
		return
	}

	output := habitat.NetworkHabitatOrgIssueInviteTokenOutput{
		Token: token,
	}
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}

func (s *Server) MintMemberIdentity(w http.ResponseWriter, r *http.Request) {
	// no authn/authz: this is called by new members who don't exist yet
	var req habitat.NetworkHabitatOrgMintMemberIdentityInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	if req.Token == "" || req.Handle == "" || req.OrgId == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	orgDid, err := syntax.ParseDID(req.OrgId)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"parsing org did",
			http.StatusBadRequest,
		)
		return
	}

	id, err := s.store.CreateNewMemberIdentity(
		r.Context(),
		orgDid,
		req.Token,
		req.Handle,
		req.Password,
		req.LoginID,
	)
	if errors.Is(err, ErrInvalidToken) {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	} else if err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"minting member identity",
			http.StatusInternalServerError,
		)
		return
	}

	// Create a minimal app.bsky.actor.profile record so the identity is usable by atproto apps
	if s.pear != nil {
		profile := map[string]any{
			"$type":  "app.bsky.actor.profile",
			"did":    id.DID.String(),
			"handle": id.Handle.String(),
		}
		_, err = s.pear.PutRecord(
			r.Context(),
			id.DID,
			id.DID,
			syntax.NSID("app.bsky.actor.profile"),
			profile,
			syntax.RecordKey("self"),
			nil,
			[]permissions.Grantee{},
		)
		if err != nil {
			slog.ErrorContext(r.Context(),
				"failed to create profile record for new member",
				"err",
				err,
				"handle",
				id.Handle,
			)
		}
	}

	output := habitat.NetworkHabitatOrgMintMemberIdentityOutput{
		Did:    id.DID.String(),
		Handle: id.Handle.String(),
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(output); err != nil {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			err,
			"encoding response",
			http.StatusInternalServerError,
		)
		return
	}
}
