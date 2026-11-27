package pearserver

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spacestream"
)

// GetSnapshot implements network.habitat.space.getSnapshot: the event log up
// to a pinned tip, paged over plain HTTP, for catching up before following
// network.habitat.space.subscribe. See package spacestream.
func (p *PearServer) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query, err := spacestream.ParseSnapshotQuery(r.URL.Query())
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid query params", err)
		return
	}
	cred, ok := p.validator.Request(
		authn.WithMethods(
			authn.ValidatorMethodOAuth,
			authn.ValidatorMethodServiceAuth,
			authn.ValidatorMethodSpaceCredential,
		),
	).Validate(w, r)
	if !ok {
		return
	}
	reader, ok := streamReader(cred)
	if !ok {
		httpx.WriteUnauthorized(ctx, w, "credential names no user or space", nil)
		return
	}
	page, err := p.spaceStream.Snapshot(ctx, spacestream.Subscription{Reader: reader}, query)
	if errors.Is(err, spacestream.ErrCredentialMismatch) {
		httpx.WriteInvalidRequest(ctx, w, "invalid spaces filter", err)
		return
	} else if errors.Is(err, spacestream.ErrLegacyCredentialDisabled) {
		httpx.WriteUnauthorized(ctx, w, "space credential is not accepted", nil)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, err)
		return
	}
	events := make([]any, len(page.Events))
	for i, e := range page.Events {
		events[i] = json.RawMessage(e)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetSnapshotOutput{
		Events:            events,
		PlannedThroughSeq: int64(page.PlannedThroughSeq),
		TipSeq:            int64(page.TipSeq),
	})
}
