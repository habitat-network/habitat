package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/opensocial"
	orgpkg "github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/utils"
)

var errNotMemberOfOrg = errors.New("not a member of an organization")

type Server struct {
	store           orgpkg.Store
	validator       authn.RequestValidator
	domain          string
	decoder         *schema.Decoder
	dir             identity.Directory
	instancePolicy  instance.PolicyStore
	opensocialStore *opensocial.Store
}

func NewServer(
	store orgpkg.Store,
	validator authn.RequestValidator,
	domain string,
	dir identity.Directory,
	instancePolicy instance.PolicyStore,
	opensocialStore *opensocial.Store,
) (*Server, error) {
	return &Server{
		store:           store,
		validator:       validator,
		domain:          domain,
		decoder:         schema.NewDecoder(),
		dir:             dir,
		instancePolicy:  instancePolicy,
		opensocialStore: opensocialStore,
	}, nil
}

func (s *Server) IsMember(ctx context.Context, member syntax.DID) (bool, error) {
	_, err := s.store.GetOrgForDID(ctx, member)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) validateOrgToken(
	ctx context.Context,
	orgID string,
	token string,
) (orgpkg.Org, error) {
	org, err := s.store.GetOrg(ctx, syntax.DID(orgID))
	if err != nil {
		return nil, err
	}

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
	var org orgpkg.Org
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
		credInfo, ok := s.validator.Request(
			authn.WithMethods(authn.ValidatorMethodOAuth),
		).Validate(w, r)
		if !ok {
			return
		}

		org, err = s.store.GetOrgForDID(r.Context(), credInfo.Subject)
		if errors.Is(err, orgpkg.ErrMemberNotFound) {
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
	httpx.WriteJSON(r.Context(), w, meta)
}

func (s *Server) BootstrapAdmin(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("unimplemented"))
	w.WriteHeader(http.StatusNotImplemented)
}

func (s *Server) CreateOrg(w http.ResponseWriter, r *http.Request) {
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
	} else if errors.Is(err, orgpkg.ErrOrgAlreadyExists) {
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
	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) GetAdmins(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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

	httpx.WriteJSON(r.Context(), w, &habitat.NetworkHabitatOrgGetAdminsOutput{
		Admins: admins,
	})
}

func (s *Server) GetMembers(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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

	httpx.WriteJSON(r.Context(), w, &habitat.NetworkHabitatOrgGetMembersOutput{
		Members: members,
	})
}

// GetProfile implements network.habitat.org.getProfile: fetches a member's
// profile (handle plus their community.opensocial.memberProfile record, if
// any) within the authenticated caller's org. Callable by any org member.
func (s *Server) GetProfile(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatOrgGetProfileParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decoding query params", http.StatusBadRequest)
		return
	}
	memberDID, err := syntax.ParseDID(params.Did)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "parsing did", http.StatusBadRequest)
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(), w, err, "getting organization", http.StatusInternalServerError,
		)
		return
	}
	isMember, err := org.IsMember(r.Context(), memberDID)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(), w, err, "checking membership", http.StatusInternalServerError,
		)
		return
	}
	if !isMember {
		utils.LogAndHTTPError(
			r.Context(), w, errNotMemberOfOrg, "member not in org", http.StatusNotFound,
		)
		return
	}

	view, err := s.getProfileView(r.Context(), org.DID(), memberDID)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(), w, err, "getting member profile", http.StatusInternalServerError,
		)
		return
	}

	httpx.WriteJSON(r.Context(), w, &habitat.NetworkHabitatOrgGetProfileOutput{
		Did:         view.Did,
		Handle:      view.Handle,
		DisplayName: view.DisplayName,
		Bio:         view.Bio,
		AvatarUrl:   view.AvatarUrl,
	})
}

// GetProfiles implements network.habitat.org.getProfiles: fetches several
// members' profiles within the authenticated caller's org. Callable by any
// org member. Members not in the caller's org are silently omitted.
func (s *Server) GetProfiles(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var params habitat.NetworkHabitatOrgGetProfilesParams
	if err := s.decoder.Decode(&params, r.URL.Query()); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "decoding query params", http.StatusBadRequest)
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
	if err != nil {
		utils.LogAndHTTPError(
			r.Context(), w, err, "getting organization", http.StatusInternalServerError,
		)
		return
	}

	profiles := make([]habitat.NetworkHabitatOrgDefsProfileView, 0, len(params.Dids))
	for _, didStr := range params.Dids {
		memberDID, err := syntax.ParseDID(didStr)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parsing did", http.StatusBadRequest)
			return
		}
		isMember, err := org.IsMember(r.Context(), memberDID)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(), w, err, "checking membership", http.StatusInternalServerError,
			)
			return
		}
		if !isMember {
			continue
		}
		view, err := s.getProfileView(r.Context(), org.DID(), memberDID)
		if err != nil {
			utils.LogAndHTTPError(
				r.Context(), w, err, "getting member profile", http.StatusInternalServerError,
			)
			return
		}
		profiles = append(profiles, view)
	}

	httpx.WriteJSON(r.Context(), w, &habitat.NetworkHabitatOrgGetProfilesOutput{
		Profiles: profiles,
	})
}

// getProfileView resolves memberDID's handle and, if set, their
// community.opensocial.memberProfile record within orgDID into a single
// view.
func (s *Server) getProfileView(
	ctx context.Context,
	orgDID, memberDID syntax.DID,
) (habitat.NetworkHabitatOrgDefsProfileView, error) {
	id, err := s.dir.LookupDID(ctx, memberDID)
	if err != nil {
		return habitat.NetworkHabitatOrgDefsProfileView{}, fmt.Errorf(
			"looking up member handle: %w", err,
		)
	}
	profile, err := s.opensocialStore.GetMemberProfile(ctx, orgDID, memberDID)
	if err != nil {
		return habitat.NetworkHabitatOrgDefsProfileView{}, fmt.Errorf(
			"getting member profile record: %w", err,
		)
	}
	return habitat.NetworkHabitatOrgDefsProfileView{
		Did:         memberDID.String(),
		Handle:      id.Handle.String(),
		DisplayName: profile.DisplayName,
		Bio:         profile.Bio,
		AvatarUrl:   profile.AvatarUrl,
	}, nil
}

func (s *Server) AddAdmin(w http.ResponseWriter, r *http.Request) {
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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
	credInfo, ok := s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	org, err := s.store.GetOrgForDID(r.Context(), credInfo.Subject)
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
	httpx.WriteJSON(r.Context(), w, output)
}

func (s *Server) MintMemberIdentity(w http.ResponseWriter, r *http.Request) {
	var req habitat.NetworkHabitatOrgMintMemberIdentityInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "reading request body", http.StatusBadRequest)
		return
	}

	if req.Token == "" || req.Handle == "" || req.OrgId == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	orgDid, ok := httpx.ParseDIDInput(r.Context(), w, req.OrgId, "orgId")
	if !ok {
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
	if errors.Is(err, orgpkg.ErrInvalidToken) {
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

	output := habitat.NetworkHabitatOrgMintMemberIdentityOutput{
		Did:    id.DID.String(),
		Handle: id.Handle.String(),
	}
	httpx.WriteJSON(r.Context(), w, output)
}
