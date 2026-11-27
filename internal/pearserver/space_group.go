package pearserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// isGroup reports whether did is a group on this host.
func (p *PearServer) isGroup(
	ctx context.Context,
	w http.ResponseWriter,
	did syntax.DID,
) (bool, bool) {
	ok, err := p.opensocialStore.IsOrg(ctx, did)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check group: %w", err))
		return false, false
	}
	return ok, true
}

// writeGroupSpaceError answers a group space operation's error, returning
// whether err was nil.
func writeGroupSpaceError(ctx context.Context, w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, spaces.ErrSpaceAlreadyExists):
		httpx.WriteError(ctx, w, "SpaceAlreadyExists", "", http.StatusBadRequest)
	case errors.Is(err, spaces.ErrSpaceNotFound):
		httpx.WriteSpaceNotFound(ctx, w, err)
	case errors.Is(err, opensocial.ErrReservedSpace):
		httpx.WriteError(ctx, w, "ReservedSpace", err.Error(), http.StatusBadRequest)
	case errors.Is(err, opensocial.ErrUnknownRole), errors.Is(err, opensocial.ErrInvalidScope):
		httpx.WriteInvalidRequest(ctx, w, err.Error(), err)
	case errors.Is(err, opensocial.ErrEscalation):
		httpx.WriteUnauthorized(ctx, w, err.Error(), nil)
	default:
		httpx.WriteServerError(ctx, w, err)
	}
	return false
}

// createGroupSpace creates a space under group, which must be the caller's
// own OAuth credential, for com.atproto.space.createSpace with an
// org.atgrp.policy.
func (p *PearServer) createGroupSpace(
	ctx context.Context,
	w http.ResponseWriter,
	caller groupCaller,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
	policy spaces.Policy,
) {
	if !caller.Scopes.Covers("space:*?manage=create") {
		httpx.WriteUnauthorized(ctx, w, "the session may not create spaces", nil)
		return
	}
	uri, err := p.opensocialStore.CreateGroupSpace(
		ctx, caller.Group, caller.Actor, spaceType, skey, policy,
	)
	if !writeGroupSpaceError(ctx, w, err) {
		return
	}
	httpx.WriteJSON(
		ctx, w, comatproto.ComAtprotoSpaceCreateSpaceOutput{Space: uri.AtSpace().String()},
	)
}

// guardGroupWrite applies the group rules to a record write when credInfo is
// a session acting as the group that owns space, writing the response and
// returning false when the write is refused. Writes by members into their
// own repos are not affected. A nil value is a delete.
func (p *PearServer) guardGroupWrite(
	ctx context.Context,
	w http.ResponseWriter,
	credInfo *authn.CredentialInfo,
	space habitat_syntax.SpaceURI,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	value map[string]any,
) bool {
	if credInfo.Method != authn.ValidatorMethodOAuth || credInfo.Subject != space.SpaceOwner() {
		return true
	}
	group, ok := p.isGroup(ctx, w, credInfo.Subject)
	if !ok {
		return false
	}
	if !group {
		return true
	}
	caller, ok := p.callerFor(ctx, w, credInfo, credInfo.Subject)
	if !ok {
		return false
	}
	err := p.opensocialStore.AuthorizeGroupWrite(ctx, opensocial.GroupWrite{
		Group: caller.Group, Actor: caller.Actor, Scopes: caller.Scopes,
		Space: space, Collection: collection, Rkey: rkey, Value: value,
	})
	switch {
	case err == nil:
		return true
	case errors.Is(err, opensocial.ErrHostMaintained):
		httpx.WriteError(ctx, w, "HostMaintainedRecord", err.Error(), http.StatusForbidden)
	case errors.Is(err, opensocial.ErrLastAdmin),
		errors.Is(err, opensocial.ErrCannotDeleteBuiltinRole),
		errors.Is(err, opensocial.ErrInvalidRoleID):
		httpx.WriteInvalidRequest(ctx, w, err.Error(), err)
	case errors.Is(err, opensocial.ErrEscalation):
		httpx.WriteUnauthorized(ctx, w, err.Error(), nil)
	default:
		httpx.WriteServerError(ctx, w, fmt.Errorf("authorize group write: %w", err))
	}
	return false
}
