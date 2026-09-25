package pearserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ApplyWrites atomically creates records in one authenticated member's Space
// repo. Schema validation is unsupported, as with PutRecord.
func (p *PearServer) ApplyWrites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.WriteError(ctx, w, "InvalidRequest", "POST required", http.StatusMethodNotAllowed)
		return
	}
	var input habitat.NetworkHabitatSpaceApplyWritesInput
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid batch request", nil)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		httpx.WriteInvalidRequest(ctx, w, "expected one JSON object", nil)
		return
	}
	if input.Validate {
		httpx.WriteNotSupported(ctx, w, "schema validation is not yet supported")
		return
	}
	if len(input.Writes) == 0 || len(input.Writes) > spaces.MaxCreateBatch {
		httpx.WriteInvalidRequest(ctx, w, "batch must contain 1 to 100 creates", nil)
		return
	}
	space, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
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
		return
	}
	repo, ok := httpx.ParseDIDInput(ctx, w, input.Repo, "repo")
	if !ok {
		return
	}
	if cred.Subject != repo {
		httpx.WriteError(
			ctx,
			w,
			"Forbidden",
			"cannot write another member repo",
			http.StatusForbidden,
		)
		return
	}
	writes := make([]spaces.CreateWrite, 0, len(input.Writes))
	for _, entry := range input.Writes {
		if entry.Action != "create" ||
			(entry.LexiconTypeID != "" && entry.LexiconTypeID != "network.habitat.space.applyWrites#create") {
			httpx.WriteInvalidRequest(ctx, w, "only create entries are supported", nil)
			return
		}
		collection, ok := httpx.ParseNSIDInput(ctx, w, entry.Collection, "collection")
		if !ok {
			return
		}
		if habitat_syntax.ReservedCollections.Contains(collection) {
			httpx.WriteInvalidRequest(
				ctx,
				w,
				"reserved collections require their dedicated endpoints",
				nil,
			)
			return
		}
		rkey, err := syntax.ParseRecordKey(entry.Rkey)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "a valid explicit rkey is required", nil)
			return
		}
		value, ok := entry.Value.(map[string]any)
		if !ok {
			httpx.WriteInvalidRequest(ctx, w, "record value must be a JSON object", nil)
			return
		}
		record, err := spaces.MarshalRecord(value)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, "invalid record data", nil)
			return
		}
		writes = append(
			writes,
			spaces.CreateWrite{Collection: collection, Rkey: rkey, Value: record},
		)
	}
	results, err := p.spacesStore.ApplyCreates(ctx, space, repo, writes)
	switch {
	case errors.Is(err, spaces.ErrInvalidBatch):
		httpx.WriteInvalidRequest(ctx, w, "invalid create batch", nil)
	case errors.Is(err, spaces.ErrRecordAlreadyExists):
		httpx.WriteError(
			ctx,
			w,
			"RecordAlreadyExists",
			"a record key already exists",
			http.StatusConflict,
		)
	case errors.Is(err, spaces.ErrBlobNotFound):
		httpx.WriteError(ctx, w, "BlobNotFound", "blob not found", http.StatusNotFound)
	case errors.Is(err, spaces.ErrSpaceNotFound):
		httpx.WriteSpaceNotFound(ctx, w, err)
	case err != nil:
		httpx.WriteServerError(ctx, w, errors.New("atomic record creation failed"))
	default:
		output := habitat.NetworkHabitatSpaceApplyWritesOutput{
			Results: make([]habitat.NetworkHabitatSpaceApplyWritesResult, len(results)),
		}
		for i, result := range results {
			output.Results[i] = habitat.NetworkHabitatSpaceApplyWritesResult{
				Uri: result.URI.String(),
				Cid: result.CID,
			}
		}
		httpx.WriteJSON(ctx, w, output)
	}
}
