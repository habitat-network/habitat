package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
// the oplog it verifies the folded hash against the host's signed commit: a
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
	var headCommit habitat.NetworkHabitatSpaceDefsSignedCommit

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		output, err := listRepoOps(ctx, client, space, repoDID, since)
		if err != nil {
			if errors.Is(err, errRevTooFar) {
				return e.scheduleRetry(ctx, space, repoDID, stateDesynced, err)
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

		// The signed commit is only present once the head of the oplog is
		// reached; keep the latest one to verify after the loop.
		headCommit = output.Commit

		if output.Cursor == "" || len(output.Ops) == 0 {
			break
		}
	}

	// A commit with Ver 0 was omitted (empty repo, or no signer covers the
	// owner); there is nothing to verify against.
	if headCommit.Ver != 0 {
		commit, err := decodeCommit(headCommit)
		if err != nil {
			return e.scheduleRetry(ctx, space, repoDID, stateError, err)
		}
		if err := e.verifier.Verify(ctx, space, repoDID, commit, &lt); err != nil {
			if errors.Is(err, spacecommit.ErrInvalidCommit) {
				e.metrics.verified(ctx, "invalid")
				return e.scheduleRetry(ctx, space, repoDID, stateDesynced,
					fmt.Errorf("verify head commit: %w", err))
			}
			return e.scheduleRetry(ctx, space, repoDID, stateError, err)
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
func listRepoOps(
	ctx context.Context,
	client *http.Client,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	since syntax.TID,
) (habitat.NetworkHabitatSpaceListRepoOpsOutput, error) {
	var output habitat.NetworkHabitatSpaceListRepoOpsOutput

	params := url.Values{
		"space": []string{space.String()},
		"repo":  []string{repoDID.String()},
		"limit": []string{"1000"},
	}
	if since != "" {
		params.Set("since", since.String())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/xrpc/network.habitat.space.listRepoOps?"+params.Encode(), nil)
	if err != nil {
		return output, fmt.Errorf("create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return output, fmt.Errorf("list repo ops: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		var body atclient.ErrorBody
		decErr := json.NewDecoder(resp.Body).Decode(&body)
		if decErr == nil && body.Name == "RevNotFound" {
			return output, fmt.Errorf("%w: %s", errRevTooFar, body.Message)
		}
		return output, fmt.Errorf("list repo ops: %s", resp.Status)
	}
	decodeErr := json.NewDecoder(resp.Body).Decode(&output)
	closeErr := resp.Body.Close()
	if decodeErr != nil {
		return output, fmt.Errorf("decode list repo ops: %w", decodeErr)
	}
	return output, closeErr
}

// getLatestCommit fetches a repo's current signed commit from its host. A
// repo the host has no records for answers 404, which is reported as
// errEmptyRepoHead so callers can treat it as "nothing to sync" rather than a
// failure.
func getLatestCommit(
	ctx context.Context,
	client *http.Client,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) (habitat.NetworkHabitatSpaceDefsSignedCommit, error) {
	var output habitat.NetworkHabitatSpaceGetLatestCommitOutput

	params := url.Values{
		"space": []string{space.String()},
		"repo":  []string{repoDID.String()},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/xrpc/network.habitat.space.getLatestCommit?"+params.Encode(), nil)
	if err != nil {
		return output.Commit, fmt.Errorf("create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return output.Commit, fmt.Errorf("get latest commit: %w", err)
	}
	decodeErr := json.NewDecoder(resp.Body).Decode(&output)
	closeErr := resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return output.Commit, errEmptyRepoHead
	}
	if resp.StatusCode != http.StatusOK {
		return output.Commit, fmt.Errorf("get latest commit: %s", resp.Status)
	}
	if decodeErr != nil {
		return output.Commit, fmt.Errorf("decode get latest commit: %w", decodeErr)
	}
	return output.Commit, closeErr
}

// listRepoPaths pages through network.habitat.space.listRecords with values
// excluded, returning the repo's full path → CID listing. This is the cheap
// enumeration the permissioned-data protocol's narrow recovery is built on:
// it identifies the host's exact record set without transferring any values.
func listRepoPaths(
	ctx context.Context,
	client *http.Client,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) ([]habitat.NetworkHabitatSpaceListRecordsRecord, error) {
	var all []habitat.NetworkHabitatSpaceListRecordsRecord
	cursor := ""
	for {
		params := url.Values{
			"space":         []string{space.String()},
			"repo":          []string{repoDID.String()},
			"excludeValues": []string{"true"},
			"limit":         []string{"1000"},
		}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			"/xrpc/network.habitat.space.listRecords?"+params.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list records: %w", err)
		}
		var output habitat.NetworkHabitatSpaceListRecordsOutput
		decodeErr := json.NewDecoder(resp.Body).Decode(&output)
		closeErr := resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("list records: %s", resp.Status)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode list records: %w", decodeErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}

		all = append(all, output.Records...)
		if output.Cursor == "" || len(output.Records) == 0 {
			return all, nil
		}
		cursor = output.Cursor
	}
}

// getRecord fetches a single record's value.
func getRecord(
	ctx context.Context,
	client *http.Client,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) (habitat.NetworkHabitatSpaceGetRecordOutput, error) {
	var output habitat.NetworkHabitatSpaceGetRecordOutput

	params := url.Values{
		"space":      []string{space.String()},
		"repo":       []string{repoDID.String()},
		"collection": []string{collection.String()},
		"rkey":       []string{rkey.String()},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"/xrpc/network.habitat.space.getRecord?"+params.Encode(), nil)
	if err != nil {
		return output, fmt.Errorf("create request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return output, fmt.Errorf("get record: %w", err)
	}
	decodeErr := json.NewDecoder(resp.Body).Decode(&output)
	closeErr := resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return output, fmt.Errorf("get record %s/%s: %s", collection, rkey, resp.Status)
	}
	if decodeErr != nil {
		return output, fmt.Errorf("decode get record: %w", decodeErr)
	}
	return output, closeErr
}
