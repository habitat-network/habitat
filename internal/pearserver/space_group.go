package pearserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// isGroup reports whether did is a group on this host.
func (p *PearServer) isGroup(ctx context.Context, w http.ResponseWriter, did syntax.DID) (bool, bool) {
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
	group syntax.DID,
	spaceType syntax.NSID,
	skey habitat_syntax.SpaceKey,
	policy spaces.Policy,
) {
	uri, err := p.opensocialStore.CreateGroupSpace(ctx, group, group, spaceType, skey, policy)
	if !writeGroupSpaceError(ctx, w, err) {
		return
	}
	httpx.WriteJSON(
		ctx, w, comatproto.ComAtprotoSpaceCreateSpaceOutput{Space: uri.AtSpace().String()},
	)
}
