package pearserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// applyWritesOp is the union of the create/update/delete write ops. The
// concrete op is chosen by the fragment of $type ("...applyWrites#create"), so
// the network.habitat and com.atproto NSID prefixes are both accepted.
type applyWritesOp struct {
	Type       string `json:"$type"`
	Collection string `json:"collection"`
	Rkey       string `json:"rkey"`
	Value      any    `json:"value"`
}

func (p *PearServer) ApplyWrites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceApplyWritesInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	if input.Validate {
		httpx.WriteNotSupported(ctx, w, "validate is not yet supported")
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
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

	writes := make([]spaces.Write, len(input.Writes))
	for i, raw := range input.Writes {
		write, err := parseApplyWritesOp(raw)
		if err != nil {
			httpx.WriteInvalidRequest(ctx, w, fmt.Sprintf("writes[%d]: %v", i, err), err)
			return
		}
		writes[i] = write
	}

	results, err := p.spacesStore.ApplyWrites(ctx, spaceURI, repo, writes)
	switch {
	case errors.Is(err, spaces.ErrSpaceNotFound):
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	case errors.Is(err, spaces.ErrRecordNotFound):
		httpx.WriteRecordNotFound(ctx, w, err)
		return
	case errors.Is(err, spaces.ErrRecordAlreadyExists):
		httpx.WriteRecordAlreadyExists(ctx, w, err)
		return
	case err != nil:
		httpx.WriteServerError(ctx, w, fmt.Errorf("apply writes: %w", err))
		return
	}

	out := habitat.NetworkHabitatSpaceApplyWritesOutput{
		Results: make([]interface{}, len(results)),
	}
	for i, res := range results {
		switch writes[i].Action {
		case spaces.WriteCreate:
			out.Results[i] = habitat.NetworkHabitatSpaceApplyWritesCreateResult{
				Uri: res.URI.String(), Cid: res.Cid.String(),
			}
		case spaces.WriteUpdate:
			out.Results[i] = habitat.NetworkHabitatSpaceApplyWritesUpdateResult{
				Uri: res.URI.String(), Cid: res.Cid.String(),
			}
		default:
			out.Results[i] = habitat.NetworkHabitatSpaceApplyWritesDeleteResult{}
		}
	}
	httpx.WriteJSON(ctx, w, out)
}

// parseApplyWritesOp validates one entry of the writes union and converts it
// into a store write.
func parseApplyWritesOp(raw any) (spaces.Write, error) {
	var zero spaces.Write
	bytes, err := json.Marshal(raw)
	if err != nil {
		return zero, err
	}
	var op applyWritesOp
	if err := json.Unmarshal(bytes, &op); err != nil {
		return zero, err
	}
	_, frag, _ := strings.Cut(op.Type, "#")
	var action spaces.WriteAction
	switch frag {
	case "create":
		action = spaces.WriteCreate
	case "update":
		action = spaces.WriteUpdate
	case "delete":
		action = spaces.WriteDelete
	default:
		return zero, fmt.Errorf("unknown write type %q", op.Type)
	}

	collection, err := syntax.ParseNSID(op.Collection)
	if err != nil {
		return zero, fmt.Errorf("invalid collection: %w", err)
	}
	if habitat_syntax.ReservedCollections.Contains(collection) {
		return zero, errors.New(
			"relationship tuples must be managed via network.habitat.relationship.* endpoints",
		)
	}
	write := spaces.Write{Action: action, Collection: collection}
	if op.Rkey != "" {
		rkey, err := syntax.ParseRecordKey(op.Rkey)
		if err != nil {
			return zero, fmt.Errorf("invalid rkey: %w", err)
		}
		write.Rkey = rkey
	} else if action != spaces.WriteCreate {
		return zero, errors.New("rkey is required")
	}
	if action == spaces.WriteDelete {
		return write, nil
	}
	value, ok := op.Value.(map[string]any)
	if !ok {
		return zero, errors.New("value must be a JSON object")
	}
	write.Value, err = spaces.MarshalRecord(value)
	if err != nil {
		return zero, fmt.Errorf("invalid record: %w", err)
	}
	return write, nil
}
