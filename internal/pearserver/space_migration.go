package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	// starContentType is the media type of the STAR archives the migration
	// endpoints exchange.
	starContentType = "application/vnd.dasl.star"
	// maxRepoImportBytes caps the archive importRepo reads.
	maxRepoImportBytes = 512 << 20
	// maxSpaceMetaImportBytes caps the archive importSpaceMeta reads. A space's
	// configuration and writer set are small next to its records.
	maxSpaceMetaImportBytes = 16 << 20
)

// ClearRecords implements com.atproto.space.clearRecords: it deletes every
// record the caller's repo holds in a space. Records in reserved collections
// are the space's configuration, managed through their own endpoints, and
// stay.
func (p *PearServer) ClearRecords(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	var input comatproto.ComAtprotoSpaceClearRecordsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}
	if credInfo.Subject != repo {
		httpx.WriteInvalidRequest(ctx, w, "repo must match the authenticated user", nil)
		return
	}
	err := spaces.ClearRepo(ctx, p.spacesStore, spaceURI, repo)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("clear records: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, comatproto.ComAtprotoSpaceClearRecordsOutput{})
}

// ImportRepo implements com.atproto.space.importRepo: it replaces the caller's
// repo in the archive's space with the archive's records, for account
// migration. The space must exist here already (for a space this host
// manages, restore it first with importSpaceMeta).
func (p *PearServer) ImportRepo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	archive, err := spaces.ReadRepoStar(http.MaxBytesReader(w, r.Body, maxRepoImportBytes))
	if errors.Is(err, spaces.ErrInvalidRepoArchive) {
		httpx.WriteError(ctx, w, "InvalidRepoFile", err.Error(), http.StatusBadRequest)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("read repo archive: %w", err))
		return
	}
	if archive.Commit.Repo != credInfo.Subject {
		httpx.WriteError(
			ctx, w, "RepoMismatch",
			"the repo does not belong to the authenticated account", http.StatusBadRequest,
		)
		return
	}
	err = spaces.ImportRepo(
		ctx, p.spacesStore, archive.Commit.Space, credInfo.Subject, archive.Records,
	)
	switch {
	case errors.Is(err, spaces.ErrSpaceNotFound):
		httpx.WriteError(ctx, w, "SpaceNotFound", "the space has not been imported yet",
			http.StatusBadRequest)
	case errors.Is(err, spaces.ErrReservedCollection):
		httpx.WriteError(ctx, w, "InvalidRepoFile", err.Error(), http.StatusBadRequest)
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("import repo: %w", err))
	}
}

// ExportSpaceMeta implements com.atproto.space.exportSpaceMeta: the space's
// policy, member list and writer set as a STAR archive, for importSpaceMeta on
// the space's next host. Only the space's owner may export it.
func (p *PearServer) ExportSpaceMeta(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params comatproto.ComAtprotoSpaceExportSpaceMetaParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	if _, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleOwner),
	).Validate(w, r); !ok {
		return
	}
	meta, err := p.exportSpaceMeta(r, spaceURI)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("export space meta: %w", err))
		return
	}
	archive, err := spaces.WriteSpaceMetaStar(*meta)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("serialize space meta: %w", err))
		return
	}
	w.Header().Set("Content-Type", starContentType)
	if _, err := w.Write(archive); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("write space meta: %w", err))
	}
}

func (p *PearServer) exportSpaceMeta(
	r *http.Request,
	uri habitat_syntax.SpaceURI,
) (*spaces.SpaceMeta, error) {
	ctx := r.Context()
	policy, err := p.spacesStore.GetSpacePolicy(ctx, uri)
	if err != nil {
		return nil, err
	}
	meta := &spaces.SpaceMeta{Space: uri, Policy: policy}
	if policy.Type == spaces.PolicyMemberSpace {
		if meta.Members, err = p.simpleStore.ListMembers(ctx, uri); err != nil {
			return nil, fmt.Errorf("list members: %w", err)
		}
	}
	// Removed writers are left behind: the archive carries the writer set
	// itself, so removals syncers have not seen yet are dropped with them.
	repos, err := p.spacesStore.ListRepos(ctx, uri)
	if err != nil {
		return nil, fmt.Errorf("list writers: %w", err)
	}
	for _, repo := range repos {
		rev, err := syntax.ParseTID(repo.Rev)
		if err != nil {
			return nil, fmt.Errorf("writer %s rev: %w", repo.DID, err)
		}
		meta.Writers = append(meta.Writers, spaces.MetaWriter{
			DID: repo.DID, RepoRev: rev, Data: repo.Hash, SpaceRev: repo.SpaceRev,
		})
	}
	return meta, nil
}

// ImportSpaceMeta implements com.atproto.space.importSpaceMeta: it restores a
// space from an exportSpaceMeta archive, creating it on this host if need be
// and replacing the policy and member list it held. Writers become tracked
// repos at the archive's revisions; each writer's records arrive with its own
// importRepo.
//
// A space that does not exist here may only be created by its authority's own
// account. For one that does, the caller must own it.
func (p *PearServer) ImportSpaceMeta(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}
	meta, err := spaces.ReadSpaceMetaStar(
		http.MaxBytesReader(w, r.Body, maxSpaceMetaImportBytes),
	)
	switch {
	case errors.Is(err, spaces.ErrUnsupportedPolicy):
		httpx.WriteError(ctx, w, "UnsupportedPolicy", err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, spaces.ErrInvalidSpaceMeta):
		httpx.WriteError(ctx, w, "InvalidSpaceMeta", err.Error(), http.StatusBadRequest)
		return
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("read space meta: %w", err))
		return
	}
	if meta.Policy.Type == spaces.PolicyAtgrp ||
		habitat_syntax.IsAtgrpConfigurationSpaceType(meta.Space.SpaceType()) {
		writePolicyError(ctx, w, spaces.ErrUnsupportedPolicy)
		return
	}

	if err := p.importSpaceMeta(r, credInfo.Subject, meta); err != nil {
		switch {
		case errors.Is(err, errNotSpaceAuthority):
			httpx.WriteError(ctx, w, "NotAuthorized", err.Error(), http.StatusForbidden)
		case errors.Is(err, spaces.ErrUnsupportedPolicy):
			writePolicyError(ctx, w, err)
		default:
			httpx.WriteServerError(ctx, w, fmt.Errorf("import space meta: %w", err))
		}
	}
}

var errNotSpaceAuthority = errors.New("not authorized to import this space")

func (p *PearServer) importSpaceMeta(
	r *http.Request,
	caller syntax.DID,
	meta *spaces.SpaceMeta,
) error {
	ctx := r.Context()
	uri := meta.Space
	exists, err := p.spacesStore.CheckSpaceExists(ctx, uri)
	if err != nil {
		return fmt.Errorf("check space: %w", err)
	}
	if exists {
		isOwner, err := p.permStore.CheckUserHasSpaceRole(
			ctx, caller, uri, habitat_syntax.SpaceRoleOwner,
		)
		if err != nil {
			return fmt.Errorf("check owner: %w", err)
		} else if !isOwner {
			return errNotSpaceAuthority
		}
	} else {
		// Anyone could otherwise claim a space under another account's name.
		if caller != uri.SpaceOwner() {
			return errNotSpaceAuthority
		}
		if _, err := p.spacesStore.CreateSpace(
			ctx, uri.SpaceOwner(), uri.SpaceType(), uri.Skey(),
		); err != nil {
			return fmt.Errorf("create space: %w", err)
		}
		if _, err := p.permStore.SetUserRelation(
			ctx, caller, uri, habitat_syntax.SpaceRoleOwner,
		); err != nil {
			return fmt.Errorf("make caller owner: %w", err)
		}
	}

	if err := p.spacesStore.SetSpacePolicy(ctx, uri, meta.Policy); err != nil {
		return fmt.Errorf("set policy: %w", err)
	}
	if meta.Policy.Type == spaces.PolicyMemberSpace {
		if err := p.replaceMembers(r, caller, uri, meta.Members); err != nil {
			return err
		}
	}
	if err := p.spacesStore.ImportWriters(ctx, uri, meta.Writers); err != nil {
		return fmt.Errorf("import writers: %w", err)
	}
	return nil
}

// replaceMembers makes members the space's member list. The caller and the
// space's owner stay members regardless: removing them would lock the importer
// out of the space it is restoring.
func (p *PearServer) replaceMembers(
	r *http.Request,
	caller syntax.DID,
	uri habitat_syntax.SpaceURI,
	members []syntax.DID,
) error {
	ctx := r.Context()
	want := make(map[syntax.DID]struct{}, len(members))
	for _, did := range members {
		want[did] = struct{}{}
		if err := p.simpleStore.AddMember(ctx, uri, did); err != nil {
			return fmt.Errorf("add member %s: %w", did, err)
		}
	}
	current, err := p.simpleStore.ListMembers(ctx, uri)
	if err != nil {
		return fmt.Errorf("list members: %w", err)
	}
	for _, did := range current {
		if _, keep := want[did]; keep || did == caller {
			continue
		}
		err := p.simpleStore.RemoveMember(ctx, uri, did)
		if errors.Is(err, simplespace.ErrCannotRemoveOrg) {
			continue
		} else if err != nil {
			return fmt.Errorf("remove member %s: %w", did, err)
		}
	}
	return nil
}
