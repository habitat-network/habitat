package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// recoverRepo rebuilds a desynced repo from a full com.atproto.space.getRepo
// CAR snapshot: it fetches the CAR, recomputes the repo's LtHash from the
// recovered record set, verifies the CAR's signed commit, then emits the
// records and settles the repo active in a single transaction. This is the
// canonical recovery path for repos whose incremental sync failed
// verification.
func (e *Engine) recoverRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) error {
	client, err := e.clients.ClientForSpace(ctx, space)
	if err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("client for space: %w", err))
	}

	params := url.Values{
		"space": []string{space.String()},
		"repo":  []string{repoDID.String()},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/xrpc/com.atproto.space.getRepo?"+params.Encode(), nil)
	if err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("create request: %w", err))
	}
	resp, err := client.Do(req)
	if err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("getRepo: %s", resp.Status))
	}

	recovered, err := parseRepoCAR(resp.Body)
	if err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("parse repo car: %w", err))
	}

	var lt spacecommit.LtHash
	for _, rec := range recovered.Records {
		lt.Add(spacecommit.RecordElement(rec.Collection, rec.Rkey, rec.Cid.String()))
	}
	if err := e.verifier.Verify(ctx, space, repoDID, recovered.Commit, &lt); err != nil {
		if errors.Is(err, spacecommit.ErrInvalidCommit) {
			e.metrics.verified(ctx, "invalid")
		}
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("verify recovered repo: %w", err))
	}
	e.metrics.verified(ctx, "verified")

	err = e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		emitter := e.emitter.InTx(tx)
		for _, rec := range recovered.Records {
			value, err := json.Marshal(rec.Value)
			if err != nil {
				return fmt.Errorf("marshal record %s/%s: %w", rec.Collection, rec.Rkey, err)
			}
			uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, rec.Collection, rec.Rkey)
			if err := emitter.Emit(ctx, uri, value); err != nil {
				return err
			}
		}
		return e.settle(ctx, tx, space, repoDID, syntax.TID(recovered.Commit.Rev), lt.State())
	})
	if err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("apply recovered repo: %w", err))
	}
	return nil
}
