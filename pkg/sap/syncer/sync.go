package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// errEmptyRepoHead reports that a host has no head commit for a repo, because
// nothing has been written to it yet.
var errEmptyRepoHead = errors.New("repo has no head commit")

// errRevTooFar reports a RevNotFound from the host: our since is ahead of the
// repo head, so incremental sync cannot continue and the repo must be rebuilt
// from a full getRepo snapshot.
var errRevTooFar = errors.New("since is ahead of the repo head")

// syncRepo pulls a repo's ops incrementally with listRepoOps, folding them
// into the repo's stored LtHash state and emitting each record. At the head of
// the oplog it verifies the folded hash against the host's MAC-authenticated commit: a
// match settles the repo active with its new rev and hash; a failed
// verification marks it desynced so it is rebuilt from a full getRepo
// snapshot.
func (e *Engine) syncRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) error {
	var r repo
	if err := e.db.WithContext(ctx).
		Where("space = ? AND did = ?", space, repoDID).
		First(&r).Error; err != nil {
		return err
	}

	client, err := e.clients.ClientForSpace(ctx, space)
	if err != nil {
		return e.scheduleRetry(ctx, space, repoDID, stateError,
			fmt.Errorf("client for space: %w", err))
	}

	lt := spacecommit.Load(r.Hash)
	since := r.Rev
	var headCommit *spacecommit.Commit

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		output, commit, err := e.listRepoOps(ctx, client, space, repoDID, since)
		if err != nil {
			if errors.Is(err, errRevTooFar) {
				return e.scheduleRetry(ctx, space, repoDID, stateDesynced, err)
			}
			if isMACFailure(err) {
				e.metrics.verified(ctx, "invalid")
				return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
					fmt.Errorf("verify head commit: %w", err))
			}
			return e.scheduleRetry(ctx, space, repoDID, stateError, err)
		}

		if len(output.Ops) > 0 {
			err := e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := e.applyOps(ctx, tx, space, repoDID, &lt, output.Ops); err != nil {
					return err
				}
				lastRev := syntax.TID(output.Ops[len(output.Ops)-1].Rev)
				return tx.Model(&repo{}).
					Where("space = ? AND did = ?", space, repoDID).
					Updates(map[string]any{"rev": lastRev, "hash": lt.State()}).Error
			})
			if err != nil {
				return e.scheduleRetry(ctx, space, repoDID, stateError, err)
			}
			if output.Cursor != "" {
				since = syntax.TID(output.Cursor)
			}
		}

		// The commit is only present once the head of the oplog is reached,
		// already authenticated by its page's MAC; keep the latest one to
		// check against the folded state after the loop.
		headCommit = commit

		if output.Cursor == "" || len(output.Ops) == 0 {
			break
		}
	}

	// A nil commit means the head of the oplog was never reached (an empty
	// repo); there is nothing to verify against.
	if headCommit != nil {
		if err := checkData(*headCommit, &lt); err != nil {
			e.metrics.verified(ctx, "invalid")
			return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
				fmt.Errorf("verify head commit: %w", err))
		}
		e.metrics.verified(ctx, "verified")
	}

	return e.settle(ctx, e.db, space, repoDID, since, lt.State())
}

// applyOps folds a page of oplog entries into the running LtHash and emits
// each record carrying a value. Entries follow the lexicon's nullable cid/prev
// semantics: prev set → the previous version folds out; cid set → the new
// version folds in (absent for deletes).
func (e *Engine) applyOps(
	ctx context.Context,
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	lt *spacecommit.LtHash,
	ops []habitat.NetworkHabitatSpaceListRepoOpsOpEntry,
) error {
	emitter := e.emitter.InTx(tx)
	for _, op := range ops {
		collection, err := syntax.ParseNSID(op.Collection)
		if err != nil {
			return fmt.Errorf("parse collection %q: %w", op.Collection, err)
		}
		rkey, err := syntax.ParseRecordKey(op.Rkey)
		if err != nil {
			return fmt.Errorf("parse rkey %q: %w", op.Rkey, err)
		}

		// An operation that supersedes a record has to fold the old version out
		// of the running hash as well as the new one in, or the hash carries
		// both and the repo reads as diverged from a commit it agrees with.
		// The lexicon requires prev on every entry (null only for a create), so
		// the host is the sole authority here — no local fallback needed.
		if op.Prev != "" {
			lt.Remove(spacecommit.RecordElement(collection, rkey, op.Prev))
		}
		if op.Cid != "" {
			lt.Add(spacecommit.RecordElement(collection, rkey, op.Cid))
		}

		// Keep the path index in step with the hash, in the same transaction,
		// so recovery can tell which records it already holds.
		if op.Cid == "" {
			if err := forgetRecord(ctx, tx, space, repoDID, collection, rkey); err != nil {
				return fmt.Errorf("forget record %s/%s: %w", collection, rkey, err)
			}
		} else if err := indexRecord(
			ctx, tx, space, repoDID, collection, rkey, op.Cid); err != nil {
			return fmt.Errorf("index record %s/%s: %w", collection, rkey, err)
		}

		if op.Value == nil {
			// A delete: emit a JSON null tombstone so consumers remove their
			// copy. The URI identifies the deleted record.
			uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, collection, rkey)
			if err := emitter.Emit(ctx, uri, []byte("null")); err != nil {
				return err
			}
			continue
		}
		value, err := json.Marshal(op.Value)
		if err != nil {
			return fmt.Errorf("marshal record %s/%s: %w", collection, rkey, err)
		}
		uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, collection, rkey)
		if err := emitter.Emit(ctx, uri, value); err != nil {
			return err
		}
	}
	return nil
}

// listRepoOps performs one network.habitat.space.listRepoOps page request.
// Every page requests a MAC; a page that reaches the head of the oplog
// carries the commit, returned once its MAC verifies.
func (e *Engine) listRepoOps(
	ctx context.Context,
	client *atclient.APIClient,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	since syntax.TID,
) (habitat.NetworkHabitatSpaceListRepoOpsOutput, *spacecommit.Commit, error) {
	var output habitat.NetworkHabitatSpaceListRepoOpsOutput
	var commit *spacecommit.Commit

	params := map[string]any{
		"space": space.String(),
		"repo":  repoDID.String(),
		"limit": 1000,
	}
	if since != "" {
		params["since"] = since.String()
	}

	err := e.verifier.withMAC(ctx, client.Host, repoDID, spacecommit.CommitMACType,
		func(x *macExchange) error {
			output, commit = habitat.NetworkHabitatSpaceListRepoOpsOutput{}, nil
			h, err := getXRPC(ctx, client, "network.habitat.space.listRepoOps",
				params, x.wantMAC(), &output)
			if err != nil {
				return err
			}
			if output.Commit == nil {
				return nil
			}
			c := spacecommit.FromXRPC(*output.Commit)
			if err := x.verifyCommit(h, c, space, repoDID); err != nil {
				return err
			}
			commit = &c
			return nil
		})
	if err != nil {
		var apiErr *atclient.APIError
		if errors.As(err, &apiErr) && apiErr.Name == "RevNotFound" {
			return output, nil, fmt.Errorf("%w: %s", errRevTooFar, apiErr.Message)
		}
		return output, nil, fmt.Errorf("list repo ops: %w", err)
	}
	return output, commit, nil
}

// getLatestCommit fetches a repo's current commit from its host, verified by
// its MAC. A repo the host has no records for answers 404, which is reported
// as errEmptyRepoHead so callers can treat it as "nothing to sync" rather than
// a failure.
func (e *Engine) getLatestCommit(
	ctx context.Context,
	client *atclient.APIClient,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) (*spacecommit.Commit, error) {
	var commit *spacecommit.Commit
	err := e.verifier.withMAC(ctx, client.Host, repoDID, spacecommit.CommitMACType,
		func(x *macExchange) error {
			var output habitat.NetworkHabitatSpaceGetLatestCommitOutput
			h, err := getXRPC(ctx, client, "network.habitat.space.getLatestCommit",
				map[string]any{"space": space.String(), "repo": repoDID.String()},
				x.wantMAC(), &output,
			)
			if err != nil {
				return err
			}
			if output.Commit == nil {
				return nil
			}
			c := spacecommit.FromXRPC(*output.Commit)
			if err := x.verifyCommit(h, c, space, repoDID); err != nil {
				return err
			}
			commit = &c
			return nil
		})
	if err != nil {
		var apiErr *atclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, errEmptyRepoHead
		}
		return nil, fmt.Errorf("get latest commit: %w", err)
	}
	return commit, nil
}

// listRepoPaths pages through network.habitat.space.listRecords with values
// excluded, returning the repo's full path → CID listing. This is the cheap
// enumeration the permissioned-data protocol's narrow recovery is built on:
// it identifies the host's exact record set without transferring any values.
func listRepoPaths(
	ctx context.Context,
	client *atclient.APIClient,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) ([]habitat.NetworkHabitatSpaceListRecordsRecord, error) {
	var all []habitat.NetworkHabitatSpaceListRecordsRecord
	cursor := ""
	for {
		params := map[string]any{
			"space":         space.String(),
			"repo":          repoDID.String(),
			"excludeValues": true,
			"limit":         1000,
		}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var output habitat.NetworkHabitatSpaceListRecordsOutput
		if err := client.Get(
			ctx,
			"network.habitat.space.listRecords",
			params,
			&output,
		); err != nil {
			return nil, fmt.Errorf("list records: %w", err)
		}

		all = append(all, output.Records...)
		if output.Cursor == "" || len(output.Records) == 0 {
			return all, nil
		}
		cursor = output.Cursor
	}
}

// getRecord fetches a single record's value, which must be the version
// with the given CID: the MAC authenticates the record's URI and CID, and the
// value must hash to that CID.
func (e *Engine) getRecord(
	ctx context.Context,
	client *atclient.APIClient,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	want string,
) (habitat.NetworkHabitatSpaceGetRecordOutput, error) {
	var output habitat.NetworkHabitatSpaceGetRecordOutput
	uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, collection, rkey).String()
	err := e.verifier.withMAC(ctx, client.Host, repoDID, spacecommit.RecordMACType,
		func(x *macExchange) error {
			output = habitat.NetworkHabitatSpaceGetRecordOutput{}
			h, err := getXRPC(ctx, client, "network.habitat.space.getRecord",
				map[string]any{
					"space": space.String(), "repo": repoDID.String(),
					"collection": collection.String(), "rkey": rkey.String(),
				}, x.wantMAC(), &output)
			if err != nil {
				return err
			}
			if output.Uri != uri {
				return fmt.Errorf("%w: record is %s, not %s",
					spacecommit.ErrInvalidCommit, output.Uri, uri)
			}
			if output.Cid != want {
				return fmt.Errorf("%w: record %s is at %s, not %s",
					spacecommit.ErrInvalidCommit, uri, output.Cid, want)
			}
			c, err := cid.Parse(output.Cid)
			if err != nil {
				return fmt.Errorf("%w: parse cid: %w", spacecommit.ErrInvalidCommit, err)
			}
			return x.verifyRecord(h, uri, c)
		})
	if err != nil {
		return output, fmt.Errorf("get record %s/%s: %w", collection, rkey, err)
	}
	got, err := recordCID(output.Value)
	if err != nil {
		return output, fmt.Errorf("encode record %s/%s: %w", collection, rkey, err)
	}
	if got.String() != want {
		return output, fmt.Errorf("%w: value of %s does not match its cid",
			spacecommit.ErrInvalidCommit, uri)
	}
	return output, nil
}
