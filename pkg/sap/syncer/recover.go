package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

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
		// failure here is not itself terminal: fall through and rebuild. It
		// left the DB untouched (it runs in its own transaction), so index is
		// still an accurate view of what sap holds for the CAR path to diff
		// against.
		slog.WarnContext(ctx, "narrow recovery failed, falling back to getRepo",
			"space", space, "repo", repoDID, "err", err)
	}
	return e.recoverFromCAR(ctx, space, repoDID, index)
}

// recoverFromCAR rebuilds a repo from a full network.habitat.space.getRepo CAR
// snapshot: it fetches the CAR, recomputes the repo's LtHash from the
// recovered record set, verifies the CAR's signed commit, then emits the
// records and settles the repo active in a single transaction. held is
// whatever sap's index holds for the repo going in (empty if none): paths it
// names that are absent from the recovered snapshot were deleted and get a
// tombstone emitted, the same as recoverByDiff does for its narrow diff.
func (e *Engine) recoverFromCAR(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	held map[string]string,
) error {
	client, err := e.clients.ClientForSpace(ctx, space)
	if err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("client for space: %w", err))
	}

	var buf bytes.Buffer
	if err := client.LexDo(ctx, http.MethodGet, "", "network.habitat.space.getRepo",
		map[string]any{"space": space.String(), "repo": repoDID.String()}, nil, &buf,
	); err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
			fmt.Errorf("getRepo: %w", err))
	}

	recovered, err := parseRepoCAR(&buf)
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

	present := make(map[string]struct{}, len(recovered.Records))
	for _, rec := range recovered.Records {
		present[recordPath(rec.Collection, rec.Rkey)] = struct{}{}
	}
	var deleted []string
	for path := range held {
		if _, ok := present[path]; !ok {
			deleted = append(deleted, path)
		}
	}

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
		for _, path := range deleted {
			collection, rkey, err := parseRecordPath(path)
			if err != nil {
				return fmt.Errorf("parse held path %q: %w", path, err)
			}
			uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, collection, rkey)
			if err := emitter.Emit(ctx, uri, []byte("null")); err != nil {
				return err
			}
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
	if headCommit == nil {
		return errors.New("host returned no signed commit")
	}
	commit := spacecommit.FromXRPC(*headCommit)

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

	// Fetch only the paths whose CID sap does not already hold. Paths held
	// before but absent from this listing were deleted: they need a tombstone
	// emitted, not a fetch, mirroring the incremental sync path in sync.go.
	rows := make([]repoRecord, 0, len(paths))
	present := make(map[string]struct{}, len(paths))
	type pending struct {
		collection syntax.NSID
		rkey       syntax.RecordKey
		cid        string
	}
	var changed []pending
	for _, p := range paths {
		collection, rkey := syntax.NSID(p.Collection), syntax.RecordKey(p.Rkey)
		path := recordPath(collection, rkey)
		present[path] = struct{}{}
		rows = append(rows, repoRecord{
			Space: space, DID: repoDID,
			Collection: collection, Rkey: rkey, Cid: p.Cid,
		})
		if held[path] != p.Cid {
			changed = append(changed, pending{collection: collection, rkey: rkey, cid: p.Cid})
		}
	}
	var deleted []string
	for path := range held {
		if _, ok := present[path]; !ok {
			deleted = append(deleted, path)
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
		"paths", len(paths), "refetched", len(changed), "deleted", len(deleted))

	err = e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		emitter := e.emitter.InTx(tx)
		for _, c := range changed {
			uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, c.collection, c.rkey)
			if err := emitter.Emit(ctx, uri, values[recordPath(c.collection, c.rkey)]); err != nil {
				return err
			}
		}
		for _, path := range deleted {
			collection, rkey, err := parseRecordPath(path)
			if err != nil {
				return fmt.Errorf("parse held path %q: %w", path, err)
			}
			uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, collection, rkey)
			if err := emitter.Emit(ctx, uri, []byte("null")); err != nil {
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
