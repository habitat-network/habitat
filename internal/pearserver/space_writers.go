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
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ListWriters implements com.atproto.space.listWriters: the space's writer set
// with each repo's latest state, either a repoWrite or, for a repo the
// authority removed, a repoRemoved. The cursor is a space revision and only
// repos updated after it are listed. This host never pages, so limit is
// accepted but every matching repo is returned in one response.
func (p *PearServer) ListWriters(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var params comatproto.ComAtprotoSpaceListWritersParams
	if err := p.decoder.Decode(&params, r.URL.Query()); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, params.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = p.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}
	var since syntax.TID
	if params.Cursor != "" {
		var err error
		since, err = syntax.ParseTID(params.Cursor)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid cursor", err)
			return
		}
	}
	spaceRev, repos, err := p.spacesStore.ListWritersSince(ctx, spaceURI, since)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list writers: %w", err))
		return
	}
	views := make([]any, len(repos))
	for i, repo := range repos {
		if repo.Removed {
			views[i] = comatproto.ComAtprotoSpaceDefsRepoRemoved{
				Repo:     repo.DID.String(),
				SpaceRev: repo.SpaceRev.String(),
			}
			continue
		}
		views[i] = comatproto.ComAtprotoSpaceDefsRepoWrite{
			Repo:     repo.DID.String(),
			RepoRev:  repo.Rev,
			Data:     repo.Hash,
			SpaceRev: repo.SpaceRev.String(),
		}
	}
	httpx.WriteJSON(ctx, w, comatproto.ComAtprotoSpaceListWritersOutput{
		Repos:  views,
		Cursor: spaceRev.String(),
	})
}

// RemoveWriter implements com.atproto.space.removeWriter: the space authority
// takes a repo out of the writer set. It is a no-op for a repo that is not a
// writer, and the repo returns on its next authorized write.
func (p *PearServer) RemoveWriter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input comatproto.ComAtprotoSpaceRemoveWriterInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleOwner),
	).Validate(w, r); !ok {
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}
	err := p.spacesStore.RemoveWriter(ctx, spaceURI, repo)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("remove writer: %w", err))
		return
	}
}
