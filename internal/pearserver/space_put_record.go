package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// PutRecord implements com.atproto.space.putRecord (and
// network.habitat.space.putRecord): it creates or replaces a record. An
// omitted rkey gets a generated TID.
func (p *PearServer) PutRecord(w http.ResponseWriter, r *http.Request) {
	p.writeRecord(w, r, false /* create */)
}

// CreateRecord implements com.atproto.space.createRecord: like PutRecord, but
// an existing record at the given rkey is an error rather than replaced.
func (p *PearServer) CreateRecord(w http.ResponseWriter, r *http.Request) {
	p.writeRecord(w, r, true /* create */)
}

// writeRecord writes the record in the request body, refusing to replace an
// existing one when create is set. createRecord's input is putRecord's
// without swapRecord, so one decoder serves both.
func (p *PearServer) writeRecord(w http.ResponseWriter, r *http.Request, create bool) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpacePutRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	if input.Validate {
		httpx.WriteNotSupported(ctx, w, "validate is not yet supported")
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(r.Context(), w, input.Space, "space uri")
	if !ok {
		return
	}
	credInfo, ok := p.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleWriter),
	).Validate(w, r)
	if !ok {
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}
	if credInfo.Subject != repo {
		httpx.WriteInvalidRequest(ctx, w, "can't write to other repo", fmt.Errorf("wrong repo"))
		return
	}
	collection, ok := httpx.ParseNSIDInput(ctx, w, input.Collection, "collection")
	if !ok {
		return
	}
	if habitat_syntax.ReservedCollections.Contains(collection) {
		httpx.WriteInvalidRequest(ctx, w,
			"relationship tuples must be managed via network.habitat.relationship.* endpoints", nil)
		return
	}
	var rkey syntax.RecordKey
	if input.Rkey != "" {
		parsedRkey, err := syntax.ParseRecordKey(input.Rkey)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid rkey", err)
			return
		}
		rkey = parsedRkey
		if create {
			// Not atomic with the write below; a concurrent put can still win.
			_, err := p.spacesStore.GetRecord(ctx, spaceURI, repo, collection, rkey)
			if err == nil {
				httpx.WriteError(ctx, w, "RecordAlreadyExists", "" /* msg */, http.StatusBadRequest)
				return
			} else if !errors.Is(err, spaces.ErrRecordNotFound) && !errors.Is(err, spaces.ErrSpaceNotFound) {
				httpx.WriteServerError(ctx, w, fmt.Errorf("get record: %w", err))
				return
			}
		}
	}
	value, ok := input.Record.(map[string]any)
	if !ok {
		httpx.WriteInvalidRequest(ctx, w, "record must be a JSON object", nil)
		return
	}
	if !p.guardGroupWrite(ctx, w, credInfo, spaceURI, collection, rkey, value) {
		return
	}
	recordBytes, err := spaces.MarshalRecord(value)
	if errors.Is(err, spaces.ErrInvalidRecord) {
		httpx.WriteInvalidRequest(ctx, w, fmt.Sprintf("invalid record: %v", err), err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("marshal record: %w", err))
		return
	}
	recordURI, cid, err := p.spacesStore.PutRecord(
		ctx,
		spaceURI,
		repo,
		collection,
		rkey,
		recordBytes,
	)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("put record: %w", err))
		return
	}
	httpx.WriteJSON(r.Context(), w, habitat.NetworkHabitatSpacePutRecordOutput{
		Uri: recordURI.AtSpace().String(),
		Cid: cid.String(),
	})
}
