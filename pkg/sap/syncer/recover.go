package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// recoverRepo rebuilds a repo whose incremental sync failed verification.
//
// When sap already holds a path index for the repo it takes the narrow path
// (recoverByDiff): enumerate the host's paths with values excluded, verify
// that listing against the host's signed commit, then fetch and emit only the
// records that actually differ. That keeps both the transfer and the outbox
// traffic proportional to the damage rather than to the repo. A repo sap holds
// nothing for, or one whose narrow recovery cannot proceed, falls back to the
// full network.habitat.space.getRepo CAR snapshot.
func (e *Engine) recoverRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) error {
	index, err := recordIndex(ctx, e.db, space, repoDID)
	if err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("read record index: %w", err))
	}
	if len(index) > 0 {
		err := e.recoverByDiff(ctx, space, repoDID, index)
		if err == nil {
			return nil
		}
		// Narrow recovery is an optimization over the full snapshot, so a
		// failure here is not itself terminal: fall through and rebuild.
		slog.WarnContext(ctx, "narrow recovery failed, falling back to getRepo",
			"space", space, "repo", repoDID, "err", err)
	}
	return e.recoverFromCAR(ctx, space, repoDID)
}

// recoverFromCAR rebuilds a repo from a full network.habitat.space.getRepo CAR
// snapshot: it fetches the CAR, recomputes the repo's LtHash from the
// recovered record set, verifies the CAR's signed commit, then emits the
// records and settles the repo active in a single transaction.
func (e *Engine) recoverFromCAR(
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
		"/xrpc/network.habitat.space.getRepo?"+params.Encode(), nil)
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
		rows := make([]repoRecord, 0, len(recovered.Records))
		for _, rec := range recovered.Records {
			value, err := json.Marshal(rec.Value)
			if err != nil {
				return fmt.Errorf("marshal record %s/%s: %w", rec.Collection, rec.Rkey, err)
			}
			uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, rec.Collection, rec.Rkey)
			if err := emitter.Emit(ctx, uri, value); err != nil {
				return err
			}
			rows = append(rows, repoRecord{
				Space:      space,
				DID:        repoDID,
				Collection: rec.Collection,
				Rkey:       rec.Rkey,
				Cid:        rec.Cid.String(),
			})
		}
		if err := replaceRecordIndex(ctx, tx, space, repoDID, rows); err != nil {
			return fmt.Errorf("replace record index: %w", err)
		}
		return e.settle(ctx, tx, space, repoDID, syntax.TID(recovered.Commit.Rev), lt.State())
	})
	if err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("apply recovered repo: %w", err))
	}
	return nil
}

// recoverByDiff repairs a repo without transferring it whole, following the
// permissioned-data protocol's narrower healing path: fetch the host's signed
// commit, enumerate its paths with values excluded, and check that listing
// against the commit's hash. Only once the listing is proven to describe the
// committed state are the differing records fetched and emitted.
//
// Verifying before fetching matters: the listing is what decides which records
// sap trusts it already holds, so accepting an unverified one would let a host
// suppress updates by omitting paths.
func (e *Engine) recoverByDiff(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	held map[string]string,
) error {
	client, err := e.clients.ClientForSpace(ctx, space)
	if err != nil {
		return fmt.Errorf("client for space: %w", err)
	}

	headCommit, err := getLatestCommit(ctx, client, space, repoDID)
	if err != nil {
		return fmt.Errorf("get latest commit: %w", err)
	}
	if headCommit.Ver == 0 {
		return errors.New("host returned no signed commit")
	}
	commit, err := decodeCommit(headCommit)
	if err != nil {
		return fmt.Errorf("decode commit: %w", err)
	}

	paths, err := listRepoPaths(ctx, client, space, repoDID)
	if err != nil {
		return fmt.Errorf("list repo paths: %w", err)
	}

	var lt spacecommit.LtHash
	for _, p := range paths {
		collection, err := syntax.ParseNSID(p.Collection)
		if err != nil {
			return fmt.Errorf("parse collection %q: %w", p.Collection, err)
		}
		rkey, err := syntax.ParseRecordKey(p.Rkey)
		if err != nil {
			return fmt.Errorf("parse rkey %q: %w", p.Rkey, err)
		}
		lt.Add(spacecommit.RecordElement(collection, rkey, p.Cid))
	}
	if err := e.verifier.Verify(ctx, space, repoDID, commit, &lt); err != nil {
		if errors.Is(err, spacecommit.ErrInvalidCommit) {
			e.metrics.verified(ctx, "invalid")
		}
		return fmt.Errorf("verify listed paths: %w", err)
	}
	e.metrics.verified(ctx, "verified")

	// Fetch only the paths whose CID sap does not already hold. Paths that
	// vanished need no fetch; dropping them from the index is enough, since
	// the outbox carries record values rather than deletions.
	rows := make([]repoRecord, 0, len(paths))
	type pending struct {
		collection syntax.NSID
		rkey       syntax.RecordKey
		cid        string
	}
	var changed []pending
	for _, p := range paths {
		collection, rkey := syntax.NSID(p.Collection), syntax.RecordKey(p.Rkey)
		rows = append(rows, repoRecord{
			Space: space, DID: repoDID,
			Collection: collection, Rkey: rkey, Cid: p.Cid,
		})
		if held[recordPath(collection, rkey)] != p.Cid {
			changed = append(changed, pending{collection: collection, rkey: rkey, cid: p.Cid})
		}
	}

	values := make(map[string][]byte, len(changed))
	for _, c := range changed {
		out, err := getRecord(ctx, client, space, repoDID, c.collection, c.rkey)
		if err != nil {
			return err
		}
		value, err := json.Marshal(out.Value)
		if err != nil {
			return fmt.Errorf("marshal record %s/%s: %w", c.collection, c.rkey, err)
		}
		values[recordPath(c.collection, c.rkey)] = value
	}

	slog.InfoContext(ctx, "narrow recovery",
		"space", space, "repo", repoDID,
		"paths", len(paths), "refetched", len(changed))

	err = e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		emitter := e.emitter.InTx(tx)
		for _, c := range changed {
			uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, c.collection, c.rkey)
			if err := emitter.Emit(ctx, uri, values[recordPath(c.collection, c.rkey)]); err != nil {
				return err
			}
		}
		if err := replaceRecordIndex(ctx, tx, space, repoDID, rows); err != nil {
			return fmt.Errorf("replace record index: %w", err)
		}
		return e.settle(ctx, tx, space, repoDID, syntax.TID(commit.Rev), lt.State())
	})
	if err != nil {
		return fmt.Errorf("apply narrow recovery: %w", err)
	}
	return nil
}
