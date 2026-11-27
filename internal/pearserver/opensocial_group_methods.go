package pearserver

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
)

func strongRefOutput(ref opensocial.StrongRef) comatproto.ComAtprotoRepoStrongRefMain {
	return comatproto.ComAtprotoRepoStrongRefMain{Uri: ref.URI, Cid: ref.CID}
}

// groupActor validates the caller of a group-DID method. The group acts
// through an OAuth credential for its own DID; a member acts through service
// auth. The store decides, from the caller's scopes, what they may do.
func (p *PearServer) groupActor(
	w http.ResponseWriter,
	r *http.Request,
	group *syntax.DID,
) (syntax.DID, bool) {
	cred, ok := p.groupCaller(w, r, group)
	if !ok {
		return "", false
	}
	return cred.Subject, true
}

// groupCaller is groupActor returning the whole credential, so a handler can
// also bound what a group's own OAuth session may do by the scopes it holds.
func (p *PearServer) groupCaller(
	w http.ResponseWriter,
	r *http.Request,
	group *syntax.DID,
) (*authn.CredentialInfo, bool) {
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return nil, false
	}
	if credInfo.Method == authn.ValidatorMethodOAuth && group != nil && credInfo.Subject != *group {
		httpx.WriteUnauthorized(
			r.Context(), w, "only the group's own OAuth credential may do this", nil,
		)
		return nil, false
	}
	return credInfo, true
}

// GroupCreateSpace implements group.opensocial.createSpace.
func (p *PearServer) GroupCreateSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input struct {
		opensocial_api.GroupOpensocialCreateSpaceInput
		// Index defaults to true when omitted.
		Index *bool `json:"index"`
		// Group names the group to create the space in. The spec leaves it
		// implicit (a group-DID session); a member calling with service auth
		// must name it, and a group's own OAuth session may omit it.
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	var named *syntax.DID
	if input.Group != "" {
		group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
		if !ok {
			return
		}
		named = &group
	}
	actor, ok := p.groupActor(w, r, named)
	if !ok {
		return
	}
	group := actor
	if named != nil {
		group = *named
	}
	spaceType, ok := httpx.ParseNSIDInput(ctx, w, input.SpaceType, "spaceType")
	if !ok {
		return
	}
	skey, ok := parseSkeyInput(w, r, input.Skey)
	if !ok {
		return
	}
	created, err := p.opensocialStore.CreateGroupSpace(
		ctx, group, actor, spaceType, skey,
		spaces.Policy{Type: spaces.PolicyRelationSpace},
		input.Access, input.Index == nil || *input.Index,
	)
	switch {
	case errors.Is(err, spaces.ErrSpaceAlreadyExists):
		httpx.WriteError(ctx, w, "SpaceAlreadyExists", "", http.StatusBadRequest)
		return
	case errors.Is(err, opensocial.ErrReservedSpace):
		httpx.WriteError(ctx, w, "ReservedSpace", err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, opensocial.ErrUnknownRole):
		httpx.WriteError(ctx, w, "UnknownRole", err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, opensocial.ErrInvalidScope):
		httpx.WriteError(ctx, w, "InvalidScope", err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, opensocial.ErrEscalation):
		httpx.WriteUnauthorized(ctx, w, err.Error(), nil)
		return
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("create group space: %w", err))
		return
	}
	out := opensocial_api.GroupOpensocialCreateSpaceOutput{
		Space:  created.Space.AtSpace().String(),
		Access: strongRefOutput(created.Access),
	}
	if created.Index != nil {
		ref := strongRefOutput(*created.Index)
		out.Index = &ref
	}
	httpx.WriteJSON(ctx, w, out)
}

// GroupDeleteSpace implements group.opensocial.deleteSpace.
func (p *PearServer) GroupDeleteSpace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input opensocial_api.GroupOpensocialDeleteSpaceInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space")
	if !ok {
		return
	}
	group := spaceURI.SpaceOwner()
	actor, ok := p.groupActor(w, r, &group)
	if !ok {
		return
	}
	err := p.opensocialStore.DeleteGroupSpace(ctx, group, actor, spaceURI)
	switch {
	case errors.Is(err, spaces.ErrSpaceNotFound):
		httpx.WriteError(ctx, w, "SpaceNotFound", "", http.StatusBadRequest)
	case errors.Is(err, opensocial.ErrReservedSpace):
		httpx.WriteError(ctx, w, "ReservedSpace", err.Error(), http.StatusBadRequest)
	case errors.Is(err, opensocial.ErrEscalation):
		httpx.WriteUnauthorized(ctx, w, err.Error(), nil)
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete group space: %w", err))
	}
}

// GroupLeaveGroup implements group.opensocial.leaveGroup.
func (p *PearServer) GroupLeaveGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.GroupOpensocialLeaveGroupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
	if !ok {
		return
	}
	err := p.opensocialStore.LeaveGroup(ctx, group, credInfo.Subject)
	switch {
	case errors.Is(err, opensocial.ErrGroupNotFound):
		httpx.WriteError(ctx, w, "GroupNotFound", "", http.StatusNotFound)
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("leave group: %w", err))
	}
}

// GroupRequestJoin implements group.opensocial.requestJoin: an invitation
// admits its holder under any join policy, an open group admits anyone, and
// an invite-only group otherwise refuses.
func (p *PearServer) GroupRequestJoin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.GroupOpensocialRequestJoinInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
	if !ok {
		return
	}
	policy, err := p.opensocialStore.JoinPolicy(ctx, group)
	if errors.Is(err, opensocial.ErrGroupNotFound) {
		httpx.WriteError(ctx, w, "GroupNotFound", "", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("join policy: %w", err))
		return
	}
	// An invitation admits its recipient whatever the policy.
	_, err = p.opensocialStore.RequestJoin(ctx, group, credInfo.Subject)
	switch {
	case err == nil:
		httpx.WriteJSON(ctx, w, opensocial_api.GroupOpensocialRequestJoinOutput{Status: "admitted"})
		return
	case !errors.Is(err, opensocial.ErrInviteNotFound):
		httpx.WriteServerError(ctx, w, fmt.Errorf("redeem invite: %w", err))
		return
	case input.Invite != "":
		httpx.WriteError(ctx, w, "InvalidInvite", "no such invite", http.StatusBadRequest)
		return
	}
	switch policy {
	case "open":
		ref, _, err := p.opensocialStore.Admit(ctx, group, credInfo.Subject)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("admit: %w", err))
			return
		}
		out := opensocial_api.GroupOpensocialRequestJoinOutput{Status: "admitted"}
		if ref.URI != "" {
			membership := strongRefOutput(ref)
			out.Membership = &membership
		}
		httpx.WriteJSON(ctx, w, out)
	case "approval":
		req, err := p.opensocialStore.CreateJoinRequest(
			ctx, group, credInfo.Subject, input.Message,
		)
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("create join request: %w", err))
			return
		}
		view := joinRequestView(req)
		httpx.WriteJSON(ctx, w, opensocial_api.GroupOpensocialRequestJoinOutput{
			Status: "pending", Request: &view,
		})
	default:
		httpx.WriteError(
			ctx,
			w,
			"JoinClosed",
			opensocial.ErrJoinClosed.Error(),
			http.StatusForbidden,
		)
	}
}

// rpcScope is the scope that grants a group method call.
func rpcScope(lxm string) string { return "rpc:" + lxm }

// requireGroupRPC checks that cred's subject holds the rpc scope for lxm in
// group. The group DID itself holds every scope, but its OAuth session holds
// only the scopes the token was granted, so those bound it.
func (p *PearServer) requireGroupRPC(
	w http.ResponseWriter,
	r *http.Request,
	group syntax.DID,
	cred *authn.CredentialInfo,
	lxm string,
) bool {
	if cred.Method == authn.ValidatorMethodOAuth && cred.Subject == group {
		if !opensocial.NewScopeSet(cred.Scopes).Covers(rpcScope(lxm)) {
			httpx.WriteUnauthorized(
				r.Context(), w, fmt.Sprintf("the session was not granted the %s scope", lxm), nil,
			)
			return false
		}
		return true
	}
	scopes, err := p.opensocialStore.UserScopes(r.Context(), group, cred.Subject)
	if err != nil {
		httpx.WriteServerError(r.Context(), w, fmt.Errorf("user scopes: %w", err))
		return false
	}
	if !scopes.Covers(rpcScope(lxm)) {
		httpx.WriteUnauthorized(
			r.Context(), w, fmt.Sprintf("caller does not hold the %s scope", lxm), nil,
		)
		return false
	}
	return true
}

func joinRequestView(req opensocial.JoinRequest) opensocial_api.GroupOpensocialDefsJoinRequest {
	return opensocial_api.GroupOpensocialDefsJoinRequest{
		Member:    string(req.Member),
		Message:   req.Message,
		CreatedAt: req.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

// GroupListJoinRequests implements group.opensocial.listJoinRequests.
func (p *PearServer) GroupListJoinRequests(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var group *syntax.DID
	if raw := r.URL.Query().Get("group"); raw != "" {
		parsed, ok := httpx.ParseDIDInput(ctx, w, raw, "group")
		if !ok {
			return
		}
		group = &parsed
	}
	cred, ok := p.groupCaller(w, r, group)
	if !ok {
		return
	}
	target := cred.Subject
	if group != nil {
		target = *group
	}
	if !p.requireGroupRPC(w, r, target, cred, "group.opensocial.listJoinRequests") {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			httpx.WriteInvalidRequest(ctx, w, "limit must be 1 to 100", err)
			return
		}
		limit = n
	}
	var after *opensocial.JoinRequest
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err := decodeJoinRequestCursor(raw)
		if err != nil {
			httpx.WriteError(ctx, w, "InvalidCursor", "", http.StatusBadRequest)
			return
		}
		after = &cursor
	}
	requests, err := p.opensocialStore.ListJoinRequests(ctx, target, limit, after)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list join requests: %w", err))
		return
	}
	out := opensocial_api.GroupOpensocialListJoinRequestsOutput{
		Requests: make([]opensocial_api.GroupOpensocialDefsJoinRequest, len(requests)),
	}
	for i, req := range requests {
		out.Requests[i] = joinRequestView(req)
	}
	if len(requests) == limit {
		out.Cursor = encodeJoinRequestCursor(requests[len(requests)-1])
	}
	httpx.WriteJSON(ctx, w, out)
}

func encodeJoinRequestCursor(req opensocial.JoinRequest) string {
	return base64.RawURLEncoding.EncodeToString(
		[]byte(strconv.FormatInt(req.CreatedAt.UnixNano(), 10) + "|" + string(req.Member)),
	)
}

func decodeJoinRequestCursor(raw string) (opensocial.JoinRequest, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return opensocial.JoinRequest{}, err
	}
	nanos, member, ok := strings.Cut(string(decoded), "|")
	if !ok {
		return opensocial.JoinRequest{}, errors.New("malformed cursor")
	}
	n, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil {
		return opensocial.JoinRequest{}, err
	}
	return opensocial.JoinRequest{CreatedAt: time.Unix(0, n).UTC(), Member: syntax.DID(member)}, nil
}

// GroupDenyJoinRequest implements group.opensocial.denyJoinRequest.
func (p *PearServer) GroupDenyJoinRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input struct {
		opensocial_api.GroupOpensocialDenyJoinRequestInput
		// Group names the group; a group's own OAuth session may omit it.
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	var named *syntax.DID
	if input.Group != "" {
		group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
		if !ok {
			return
		}
		named = &group
	}
	member, ok := httpx.ParseDIDInput(ctx, w, input.Member, "member")
	if !ok {
		return
	}
	cred, ok := p.groupCaller(w, r, named)
	if !ok {
		return
	}
	group := cred.Subject
	if named != nil {
		group = *named
	}
	if !p.requireGroupRPC(w, r, group, cred, "group.opensocial.denyJoinRequest") {
		return
	}
	err := p.opensocialStore.DeleteJoinRequest(ctx, group, member)
	switch {
	case errors.Is(err, opensocial.ErrJoinRequestNotFound):
		httpx.WriteError(ctx, w, "JoinRequestNotFound", "", http.StatusNotFound)
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("deny join request: %w", err))
	}
}

// GroupCancelJoinRequest implements group.opensocial.cancelJoinRequest.
func (p *PearServer) GroupCancelJoinRequest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input opensocial_api.GroupOpensocialCancelJoinRequestInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	group, ok := httpx.ParseDIDInput(ctx, w, input.Group, "group")
	if !ok {
		return
	}
	if _, err := p.opensocialStore.JoinPolicy(
		ctx,
		group,
	); errors.Is(
		err,
		opensocial.ErrGroupNotFound,
	) {
		httpx.WriteError(ctx, w, "GroupNotFound", "", http.StatusNotFound)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("join policy: %w", err))
		return
	}
	err := p.opensocialStore.DeleteJoinRequest(ctx, group, credInfo.Subject)
	switch {
	case errors.Is(err, opensocial.ErrJoinRequestNotFound):
		httpx.WriteError(ctx, w, "JoinRequestNotFound", "", http.StatusNotFound)
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("cancel join request: %w", err))
	}
}
