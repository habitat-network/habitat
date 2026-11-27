package pearserver

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/atgrp"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func (p *PearServer) validateRecordWriter(
	w http.ResponseWriter, r *http.Request, space habitat_syntax.SpaceURI, repo syntax.DID,
) bool {
	if space.SpaceType() == atgrp.InboxSpaceType {
		return p.validateInboxWriter(w, r, space, repo)
	}
	cred, ok := p.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(space, habitat_syntax.SpaceRoleWriter),
	).Validate(w, r)
	if !ok {
		return false
	}
	if cred.Subject != repo {
		httpx.WriteInvalidRequest(r.Context(), w, "can't write to other repo", nil)
		return false
	}
	return true
}

// validateInboxWriter confines publicWrite to authenticated callers writing
// invites in their own repo. It confers no general space role.
func (p *PearServer) validateInboxWriter(
	w http.ResponseWriter, r *http.Request, space habitat_syntax.SpaceURI, repo syntax.DID,
) bool {
	ctx := r.Context()
	cred, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return false
	}
	if cred.Subject != repo || cred.Space != "" {
		httpx.WriteInvalidRequest(ctx, w, "can't write to other repo", nil)
		return false
	}
	if !p.requireLocalInboxAccount(w, r, repo) {
		return false
	}
	if space != atgrp.InboxSpace(space.SpaceOwner()) {
		httpx.WriteSpaceNotFound(ctx, w, spaces.ErrSpaceNotFound)
		return false
	}
	policy, err := p.spacesStore.GetSpacePolicy(ctx, space)
	if errors.Is(err, spaces.ErrSpaceNotFound) || (err == nil && !atgrp.IsInboxPolicy(policy)) {
		httpx.WriteSpaceNotFound(ctx, w, spaces.ErrSpaceNotFound)
		return false
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("get inbox policy: %w", err))
		return false
	}
	return true
}

// This host must not claim to host a remote account's inbox or repo. Such
// accounts can authenticate with service auth, but that does not move data
// custody from their PDS to this one.
func (p *PearServer) requireLocalInboxAccount(
	w http.ResponseWriter,
	r *http.Request,
	did syntax.DID,
) bool {
	ctx := r.Context()
	_, err := p.hive.LookupDID(ctx, did)
	if errors.Is(err, identity.ErrDIDNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, spaces.ErrSpaceNotFound)
		return false
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("check inbox account host: %w", err))
		return false
	}
	return true
}

func validateInboxRecordKey(
	w http.ResponseWriter, r *http.Request, collection syntax.NSID, rkey syntax.RecordKey,
) bool {
	if collection != atgrp.InviteCollection {
		httpx.WriteInvalidRequest(r.Context(), w, "inbox only accepts invite records", nil)
		return false
	}
	if rkey != "" {
		if _, err := syntax.ParseTID(rkey.String()); err != nil {
			httpx.WriteInvalidRequest(r.Context(), w, "invite key must be a TID", err)
			return false
		}
	}
	return true
}

// validateRecordReader grants an invite author access to only their own
// repo's invite collection. All other reads still require a read role.
func (p *PearServer) validateRecordReader(
	w http.ResponseWriter,
	r *http.Request,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
) (*authn.CredentialInfo, bool) {
	if space.SpaceType() == atgrp.InboxSpaceType && space.Skey() == "self" &&
		collection == atgrp.InviteCollection {
		cred, ok := p.validator.Request(authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		)).Validate(w, r)
		if !ok {
			return nil, false
		}
		if cred.Space == space ||
			(cred.Space == "" && (cred.Subject == space.SpaceOwner() || cred.Subject == repo)) {
			return cred, true
		}
		httpx.WriteSpaceNotFound(r.Context(), w, spaces.ErrSpaceNotFound)
		return nil, false
	}
	return p.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(space, habitat_syntax.SpaceRoleReader),
	).Validate(w, r)
}
